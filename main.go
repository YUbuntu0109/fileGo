package main

// 导入程序相关依赖包
import (
	"bufio"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// 定义配置信息的全局变量
var CONFIG *Config

// 主方法即程序入口
func main() {

	// 设置 CPU 核心数量
	runtime.GOMAXPROCS(runtime.NumCPU())

	// 设置日志的结构
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime | log.Lmicroseconds)

	// 读取配置文件
	CONFIG = readConfig()

	// Handle registers the handler for the given pattern
	http.Handle("/css/", http.FileServer(http.Dir("template")))
	http.Handle("/js/", http.FileServer(http.Dir("template")))
	http.Handle("/files/", http.FileServer(http.Dir("template")))
	http.Handle("/images/", http.FileServer(http.Dir("template")))

	// HandleFunc registers the handler function for the given pattern
	http.HandleFunc("/", index)
	http.HandleFunc("/rmfile", rmfile)
	http.HandleFunc("/upload", upload)
	http.HandleFunc("/upload/f", upload)
	http.HandleFunc("/download", download)

	// 自动打开浏览器, 并访问链接: http://localhost:8080
	OpenBrowser()

	// 监听
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Panic(err)
	}
}

// 定义项目绝对路径的全局变量
var (
	currPath = GetCurrentPath()
)

// 文件大小
type Size interface {
	Size() int64
}

// 上传文件接口
func upload(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL.String())
	// 解析参数
	r.ParseForm()
	// 加锁,写入
	if "POST" == r.Method {
		file, multi, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// 文件读取完毕后释放 I/O 资源
		defer file.Close()
		// 约束上传文件的大小
		if sizeInterface, ok := file.(Size); ok {
			if float64(sizeInterface.Size()) > CONFIG.Size {
				http.Error(w, "超过文件大小限制", 500)
				return
			}
		}
		// 文件名
		Filename := multi.Filename
		// 判断文件是否存在,
		// 当前程序使用的路径是当前项目的的绝对路径 currPath.
		// 若想要使用相对路径, 例如将 files 文件夹与项目文件夹同目录, 则可以只使用: files/%s
		// 注: 若要使用相对路径, 需要将 main.go、fileGo.conf、template 及 files 放在同一个目录, 然后运行 go run main.go 即可.
		if Exists(fmt.Sprintf(currPath + "/files/%s", Filename)) {
			if r.URL != nil && strings.HasSuffix(r.URL.String(), "upload/f") {
				if err := os.Remove(fmt.Sprintf(currPath + "/files/%s", Filename)); err != nil {
					http.Error(w, fmt.Sprintf("WARN: [%s] %s ...", Filename, err.Error()), 500)
					return
				}
			} else {
				for i := 1; i < 100; i++ {
					if !Exists(fmt.Sprintf(currPath + "/files/%s.%d", Filename, i)) {
						Filename = fmt.Sprintf("%s.%d", Filename, i)
						break
					}
				}
			}
		}
		// 在写入文件之前应先创建 files 文件夹, 否则若 files 不存在则会抛出异常哟
		if !Exists(currPath + "/files") {
			err = os.Mkdir(currPath + "/files", os.ModePerm)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		// 将源文件写入到目标文件
		f, err := os.Create(fmt.Sprintf(currPath + "/files/%s", Filename))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// 释放 I/O 资源
		defer f.Close()
		// 拷贝文件数据
		_, err = io.Copy(f, file)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// add header rename ...
		if Filename != multi.Filename {
			w.Header().Set("rename", Filename)
		}
	}
	// 重定向刷新网页
	http.Redirect(w, r, "/", http.StatusFound)
	// 返回
	return
}

// 下载文件接口
func download(w http.ResponseWriter, r *http.Request) {
	// 解析参数
	r.ParseForm()
	// 获取文件名称
	fname := Trim(r.FormValue("f"))
	// 添加头信息
	w.Header().Set("Content-Type", "multipart/form-data")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fname))
	// 判断文件是否存在
	if !Exists(fmt.Sprintf(currPath + "/files/%s", fname)) {
		http.Error(w, fmt.Sprintf("WARN: [%s] file not exists ...", fname), 500)
		return
	}
	// 写入文件流
	FileRF(fmt.Sprintf(currPath + "/files/%s", fname), func(f *os.File) {
		_, err := io.Copy(w, bufio.NewReader(f))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	})
	// 返回
	return
}

// 删除文件接口
func rmfile(w http.ResponseWriter, r *http.Request) {
	// cookie
	if _, err := r.Cookie("username"); err != nil {
		// 重定向
		http.Redirect(w, r, "/", http.StatusFound)
		// 返回
		return
	}
	// 解析参数
	r.ParseForm()
	// 获取文件名称
	fname := Trim(r.FormValue("f"))
	// 判断安装包是否存在
	if Exists(fmt.Sprintf("files/%s", fname)) && !IsBlank(fname) {
		// 删除
		Fremove(fmt.Sprintf("files/%s", fname))
	}
	// 重定向
	http.Redirect(w, r, "/", http.StatusFound)
	// 返回
	return
}

/*
	这里偷个懒...
	应该将文件信息记录到数据库或者文件中
	我这个每次都去扫描, 浪费资源.. 那么后期计划加入 SQLite3
*/
// 文件信息
type FileInfo struct {
	Id   int    // ID
	Name string // 文件名称
	Size string // 文件大小
	Date string // 上传日期
	Stat string // 权限状态
}

// 权限信息
type Data struct {
	// 权限状态
	Stat string
	// 文件列表
	Files []*FileInfo
}

// 构造
func NewData() *Data {
	data := new(Data)
	data.Files = make([]*FileInfo, 0)
	return data
}

// web主页
func index(w http.ResponseWriter, r *http.Request) {
	// 解析参数
	r.ParseForm()
	// 管理员
	var admin string
	// form
	if _, ok := r.Form[CONFIG.Admin]; ok {
		// cookie
		cookie := http.Cookie{Name: "username", Value: CONFIG.Admin, Expires: time.Now().Add(24 * time.Hour)}
		// cookie
		http.SetCookie(w, &cookie)
		// 管理员
		admin = CONFIG.Admin
	}
	// cookie
	if cookie, err := r.Cookie("username"); err == nil {
		// 权限
		if cookie.Value == CONFIG.Admin {
			// 管理员
			admin = cookie.Value
		}
	}
	// 获取文件名称
	fname := Trim(r.FormValue("f"))
	// 创建返回对象
	data := NewData()
	data.Stat = admin
	// ID
	var id int
	// 遍历本地文件
	filepath.Walk("files", func(ph string, f os.FileInfo, err error) error {
		// 文件不存在
		if f == nil {
			return nil
		}
		// 跳过文件夹
		if f.IsDir() {
			return nil
		}
		// 判断文件是否存在
		if IsBlank(fname) {
			// 累加
			id++
			// 记录文件
			data.Files = append(data.Files, &FileInfo{id, f.Name(), unitCapacity(f.Size()), f.ModTime().Format("2006-01-02 15:04:05"), admin})
		} else {
			// 检查包含
			if strings.Contains(strings.ToLower(f.Name()), strings.ToLower(fname)) {
				// 累加
				id++
				// 记录文件
				data.Files = append(data.Files, &FileInfo{id, f.Name(), unitCapacity(f.Size()), f.ModTime().Format("2006-01-02 15:04:05"), admin})
			}
		}
		// 返回
		return nil
	})
	// 解析主页面
	t, err := template.ParseFiles("template/default.html")
	if err != nil {
		// 输出错误信息
		http.Error(w, err.Error(), 500)
		return
	}
	// 执行
	t.Execute(w, data)
	// 返回
	return
}

// 计算文件大小
func unitCapacity(size int64) string {
	if g := float64(size) / (1024 * 1024 * 1024); int64(g) > 0 {
		return fmt.Sprintf("%.2fG", g)
	} else if m := float64(size) / (1024 * 1024); int64(m) > 0 {
		return fmt.Sprintf("%.2fM", m)
	} else if k := float64(size) / (1024); int64(k) > 0 {
		return fmt.Sprintf("%.2fK", k)
	} else {
		return fmt.Sprintf("%dB", size)
	}
}

// 配置信息, 详见文件: fileGo.conf
type Config struct {
	Size  float64 `json:"size"`
	Admin string  `json:"admin"`
}

// 读取配置文件中的内容
func readConfig() *Config {
	// New ServerConf
	conf := new(Config)
	// 1073741824/1024/1024/1024 = 1G
	conf.Size = 1073741824
	conf.Admin = "admin"
	if !Exists("fileGo.conf") {
		log.Println("use default")
		log.Println("not found fileGo.conf")
		return conf
	}
	// 打开配置文件 fileGo.conf
	f, err := os.Open("fileGo.conf")
	if err != nil {
		log.Println("use default")
		log.Println(err.Error())
		return conf
	}
	// 读取配置文件
	bs, err := ioutil.ReadAll(bufio.NewReader(f))
	if err != nil {
		log.Println("use default")
		log.Println(err.Error())
		return conf
	}
	// 解析配置文件
	err = json.Unmarshal(bs, &conf)
	if err != nil {
		log.Println("use default")
		log.Println(err.Error())
		return conf
	}
	return conf
}

// 判断一个路径是否存在
func Exists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// 去掉一个字符串左右的空白串，即( 0x00 - 0x20 之内的字符均为空白字符 )
// 与 strings.TrimSpace 功能一致
func Trim(s string) string {
	size := len(s)
	if size <= 0 {
		return s
	}
	l := 0
	for ; l < size; l++ {
		b := s[l]
		if !IsSpace(b) {
			break
		}
	}
	r := size - 1
	for ; r >= l; r-- {
		b := s[r]
		if !IsSpace(b) {
			break
		}
	}
	return string(s[l : r+1])
}

// 删除文件
func Fremove(ph string) (err error) {
	err = os.Remove(ph)
	return err
}

/*
将从自己磁盘目录, 只读的方式打开一个文件, 如果文件不存在或者打开错误, 则返回 nil 且打印 log
调用者将负责关闭文件
*/
func FileR(ph string) *os.File {
	f, err := os.Open(ph)
	if nil != err {
		return nil
	}
	return f
}

// 用回调的方式打文件以便读取内容, 回调函数不需要关心文件关闭等问题
func FileRF(ph string, callback func(*os.File)) {
	f := FileR(ph)
	if nil != f {
		defer f.Close()
		callback(f)
	}
}

// 判断是否未空字符
func IsSpace(c byte) bool {
	if c >= 0x00 && c <= 0x20 {
		return true
	}
	return false
}

// 判断一个字符串是不是空白串, 即( 0x00 - 0x20 之内的字符均为空白字符 )
func IsBlank(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !IsSpace(b) {
			return false
		}
	}
	return true
}

// 获取当前项目的绝对路径
// C:/Users/Administrator.DESKTOP-3V51O0O/Desktop/Go/workbench/godw
func GetCurrentPath() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return strings.Replace(dir, "\\", "/", -1)
}

// 程序运行成功后, 控制台输出 logo
func PrintLogo()  {

}

// 程序运行成功后, 自动打开浏览器并访问: https://localhost:8080
func OpenBrowser() {
	// Run on windows os
	cmd := exec.Command(`cmd`,`/c`, `start`, `http://localhost:8080`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err := cmd.Start()
	if err != nil {
		fmt.Println("error: couldn't have opened the browser bro", err.Error())
		return
	}
}

// 新建文件夹
func NewFolder() {

}

// 文件数据加密
func FileEncrypt() {

}

// 实时更新页面
func UpdateWeb() {

}
