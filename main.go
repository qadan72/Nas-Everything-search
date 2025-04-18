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

func initDB(dbPath string) error {
	log.Println("初始化数据库...")
	// 打开数据库文件，如果文件不存在会创建
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_sync=NORMAL&_vacuum=INCREMENTAL")
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

	// 创建表和索引，确保表存在
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
	if err != nil {
		return fmt.Errorf("创建表格失败: %v", err)
	}

	log.Println("数据库初始化完成，表格已创建或已存在")
	return nil
}

func processPath(inputPath string) (string, error) {
	log.Println("处理路径:", inputPath)
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

func scanAndSave(configPath, dbPath string, done chan bool) {
	log.Println("开始扫描文件...")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 开始事务
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
	var totalFiles int64
	var scannedFiles int64

	// 先遍历一遍，计算文件总数
	err = filepath.Walk(configPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		totalFiles++
		return nil
	})
	if err != nil {
		tx.Rollback()
		log.Fatal(err)
	}

	log.Printf("文件扫描开始，共需要扫描 %d 个文件\n", totalFiles)

	// 执行文件扫描并保存到数据库
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
		if err != nil {
			return err
		}

		// 更新扫描进度，每30秒输出一次
		scannedFiles++
		if scannedFiles%1000 == 0 { // 每1000个文件输出一次进度
			log.Printf("扫描进度: %d/%d 文件已扫描...\n", scannedFiles, totalFiles)
		}

		return nil
	})

	if err != nil {
		tx.Rollback()
		log.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}

	log.Println("文件扫描完成，数据已更新")
	done <- true // 完成扫描
}

func main() {
	// 获取可执行文件所在目录
    exePath, err := os.Executable()
    if err != nil {
        log.Fatal("无法获取可执行文件路径: ", err)
    }
    exeDir := filepath.Dir(exePath)
    configEnvPath := filepath.Join(exeDir, "config.env") // 拼接完整路径

    log.Println("加载配置文件:", configEnvPath)
    // 加载配置文件
    err = godotenv.Load(configEnvPath)
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

	log.Println("检查数据库是否已存在...")

	// 获取数据库文件路径
	dbPath := filepath.Join(exeDir, "sql.db")

	// 检查 sql.db 是否存在，如果存在则跳过首次扫描
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Println("检测到 sql.db 文件不存在，执行首次扫描...")
		done := make(chan bool)

		// 确保数据库初始化成功
		if err := initDB(dbPath); err != nil {
			log.Fatal("数据库初始化失败:", err)
		}

		// 执行文件扫描
		go scanAndSave(configPath, dbPath, done)

		// 等待扫描完成
		<-done
		log.Println("首次扫描完成，数据库已更新。")
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

	log.Printf("设置定时任务，每天 %02d:%02d 执行扫描任务\n", hour, minute)

	// 定时任务
	cronScheduler := cron.New()
	cronExp := fmt.Sprintf("%d %d * * *", minute, hour)
	_, err = cronScheduler.AddFunc(cronExp, func() {
		log.Println("开始定时文件扫描...")
		done := make(chan bool)
		go scanAndSave(configPath, dbPath, done)

		// 等待扫描完成
		<-done
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

		db, err := sql.Open("sqlite", dbPath)
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
				WHERE f.filename LIKE ?`
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
