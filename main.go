package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"github.com/rs/cors" // 引入 CORS 库
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	_ "modernc.org/sqlite"
)

type FileInfo struct {
	Path       string `json:"path"`
	FileName   string `json:"filename"`
	Size       int64  `json:"size"`
	CreateTime string `json:"create_time"`
}

func initDB() error {
	db, err := sql.Open("sqlite", "./sql.db?_journal=WAL&_sync=NORMAL&_vacuum=INCREMENTAL")
	if err != nil {
		return err
	}
	defer db.Close()

	// 启用压缩和优化设置
	_, err = db.Exec(`
		PRAGMA auto_vacuum = INCREMENTAL;
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
	`)
	if err != nil {
		return fmt.Errorf("启用SQLite优化设置失败: %v", err)
	}

	// 创建表和索引
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS paths (
			id INTEGER PRIMARY KEY,
			path TEXT NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS files (
			id INTEGER PRIMARY KEY,
			path_id INTEGER NOT NULL,
			filename TEXT NOT NULL,
			size INTEGER,
			create_time DATETIME,
			FOREIGN KEY(path_id) REFERENCES paths(id)
		);
		CREATE INDEX IF NOT EXISTS idx_filename ON files(filename);
	`)
	return err
}

func processPath(inputPath string) (string, error) {
	cleanPath := filepath.ToSlash(inputPath)
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", fmt.Errorf("路径解析失败: %v", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("目标目录不存在: %s", absPath)
	}

	return filepath.ToSlash(absPath), nil
}

func scanAndSave(configPath string) {
	db, err := sql.Open("sqlite", "./sql.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	// 清空表
	_, err = tx.Exec("DELETE FROM files; DELETE FROM paths")
	if err != nil {
		tx.Rollback()
		log.Fatal(err)
	}

	// 路径缓存
	pathCache := make(map[string]int64)

	err = filepath.Walk(configPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// 提取路径和文件名
		dir := filepath.ToSlash(filepath.Dir(path))
		filename := filepath.Base(path)

		// 获取路径 ID
		pathID, ok := pathCache[dir]
		if !ok {
			res := tx.QueryRow("INSERT OR IGNORE INTO paths (path) VALUES (?) RETURNING id", dir)
			if err := res.Scan(&pathID); err != nil {
				return err
			}
			pathCache[dir] = pathID
		}

		// 插入文件记录
		_, err = tx.Exec(
			"INSERT INTO files (path_id, filename, size, create_time) VALUES (?, ?, ?, ?)",
			pathID,
			filename,
			info.Size(),
			info.ModTime().Format(time.RFC3339),
		)
		return err
	})

	if err != nil {
		tx.Rollback()
		log.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}

	log.Println("文件扫描完成，数据已更新")
}

func main() {
	err := godotenv.Load("config.env")
	if err != nil {
		log.Fatal("加载配置文件失败: ", err)
	}

	configPath, err := processPath(os.Getenv("path"))
	if err != nil {
		log.Fatal("路径处理错误:", err)
	}

	configTime := os.Getenv("time")
	if configTime == "" {
		log.Fatal("配置文件中缺少time字段")
	}

	if err := initDB(); err != nil {
		log.Fatal("数据库初始化失败:", err)
	}

	// 检查 sql.db 是否存在，如果存在则不执行首次扫描
	if _, err := os.Stat("./sql.db"); os.IsNotExist(err) {
		log.Println("执行首次目录扫描...")
		scanAndSave(configPath)
	} else {
		log.Println("检测到已存在 sql.db 文件，跳过首次扫描")
	}

	timeParts := strings.Split(configTime, ":")
	if len(timeParts) != 2 {
		log.Fatal("时间格式应为HH:MM")
	}

	hour, err := strconv.Atoi(timeParts[0])
	if err != nil || hour < 0 || hour > 23 {
		log.Fatal("无效的小时数")
	}

	minute, err := strconv.Atoi(timeParts[1])
	if err != nil || minute < 0 || minute > 59 {
		log.Fatal("无效的分钟数")
	}

	// 定时任务
	cronScheduler := cron.New()
	cronExp := fmt.Sprintf("%d %d * * *", minute, hour)
	_, err = cronScheduler.AddFunc(cronExp, func() {
		log.Println("开始定时文件扫描...")
		scanAndSave(configPath)
	})
	if err != nil {
		log.Fatal("创建定时任务失败: ", err)
	}
	cronScheduler.Start()
	defer cronScheduler.Stop()

	// HTTP 路由
	http.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		queryType := r.URL.Query().Get("type")

		if key == "" || queryType == "" {
			http.Error(w, "缺少查询参数", http.StatusBadRequest)
			return
		}

		db, err := sql.Open("sqlite", "./sql.db")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer db.Close()

		var query string
		switch queryType {
		case "file":
			query = `
				SELECT p.path, f.filename, f.size, f.create_time
				FROM files f
				JOIN paths p ON f.path_id = p.id
				WHERE f.filename LIKE ?
			`
		default:
			http.Error(w, "无效的查询类型", http.StatusBadRequest)
			return
		}

		rows, err := db.Query(query, "%"+key+"%")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var files []FileInfo
		for rows.Next() {
			var fi FileInfo
			var createTime string
			if err := rows.Scan(&fi.Path, &fi.FileName, &fi.Size, &createTime); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fi.CreateTime = createTime
			files = append(files, fi)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(files)
	})

	// 创建 CORS 中间件
	corsMiddleware := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"}, // 允许的域名
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"}, // 允许的 HTTP 方法
		AllowedHeaders:   []string{"*"},                     // 允许的请求头
		AllowCredentials: true,                              // 允许携带凭证（如 Cookie）
		Debug:            true,                              // 调试模式
	})

	// 使用 CORS 中间件包装路由
	handler := corsMiddleware.Handler(http.DefaultServeMux)

	// 启动服务器
	log.Println("服务器启动，监听端口 :8899")
	log.Fatal(http.ListenAndServe(":8899", handler))
}