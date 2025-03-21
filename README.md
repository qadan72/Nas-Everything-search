### 起因

买了个低配的联不想内存2G的NAS，系统自带的搜索功能太慢，搜个文件得一两分钟，好在之前解锁了SSH，有了ROOT高级权限；就想着仿造Everything原理开发一个Linux搜索的快捷工具；

### 注意：

此工具专为基于Linux系统的NAS设备开发，不推荐Windows或Mac使用，请使用更强大的Everything代替。

### 使用

我的系统是Linux ARM64，请根据你的系统架构，下载release安装包，上传到指定目录直接运行；将html文件上传到指定Nginx目录，不同NAS设备请自行研究。

```bash
# 授权读写权限
chmod 755 ./Nas-Everything-search-Linux-arm64

# 执行程序
./Nas-Everything-search-Linux-arm64

# 运行后开始检索指定目录，检索完毕会生成sql.db文件；
#当前目录如果存在sql.db就会跳过检索，根据time字段进行定时更新
```

### 配置（config.env）

```bash
# Linux需要检索的目录,已兼容Windows目录，但不推荐在Windows上使用
path=/sata/home
# 定时更新目录数据的时间；当指定目录新增或删除文件后，每晚23点更新数据文件
time=23:00
```



### 搜索接口

```bash
http://192.168.10.1:8899/get?key=搜索关键字&type=file
```

| 参数名 | 示例值  | 参数类型 | 是否必填 |   参数描述   |
| :----: | :-----: | :------: | :------: | :----------: |
|  key   | example |  string  |    是    |    搜索值    |
|  type  |  file   |  string  |    是    | 无用，未开发 |



### 搜索页面展示

| <img src="images/image1.png" alt="image1" style="zoom:50%;" /> | <img src="images/image2.png" alt="image2" style="zoom:67%;" /> |
| ------------------------------------------------------------ | ------------------------------------------------------------ |
|                                                              |                                                              |





