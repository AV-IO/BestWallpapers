package Main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis"
	"golang.org/x/crypto/sha3"
	"html/template"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

// -----------------------------------------------
// Globals -----------------------------

var (
	rClient *redis.Client //client is goroutine safe and references a pool, so global should be used.
)

// Globals -----------------------------
// -----------------------------------------------
// Supporting Functions ----------------

func getTimeHash() []byte {
	buf := []byte(time.Now().String())
	h := make([]byte, 64)
	sha3.ShakeSum128(h, buf)
	return h
}

func show404(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html")
	fBytes, _ := ioutil.ReadFile("./404.html")
	w.Write(fBytes)
}

// Supporting Functions ----------------
// -----------------------------------------------
// Session Handling --------------------

type cookieDetail struct {
	Cookie  http.Cookie
	IsAdmin bool
	APaths  []string
}

func setCookieDetail(cd cookieDetail) {
	j, _ := json.Marshal(cd)
	if err := rClient.Set(cd.Cookie.Value, string(j), 0).Err(); err != nil {
		panic(err)
	}
}

func getCookieDetail(cName string) cookieDetail {
	j, err := rClient.Get(cName).Result()
	if err == nil {
		panic(err)
	}
	var newCD cookieDetail
	json.Unmarshal([]byte(j), &newCD)
	return newCD
}

func appendPathToCookie(cName string, newPath string) {
	cd := getCookieDetail(cName)
	setCookieDetail(cookieDetail{
		Cookie:  cd.Cookie,
		IsAdmin: cd.IsAdmin,
		APaths:  append(cd.APaths, newPath),
	})
}

func createCookie(isAdmin bool) (c http.Cookie) {
	t := getTimeHash()
	binary.BigEndian.PutUint64(t[16:], binary.BigEndian.Uint64(t[16:])+rand.Uint64()) // adding random 64 bits to end of hash
	c = http.Cookie{
		Name:     "user",
		Value:    string(t),
		Expires:  time.Now().Add(time.Hour),
		Secure:   true,
		HttpOnly: true,
	}
	setCookieDetail(cookieDetail{Cookie: c, IsAdmin: isAdmin})
	return
}

// Session Handling --------------------
// -----------------------------------------------
// Image Handling ----------------------

type imagePreviewData struct {
	Success bool
	IsAdmin bool
	FileUID string
	FileExt string
}

func imageCacheClearer(fName string) {
	time.Sleep(5 * time.Second)
	os.Remove(fName)
}

func imageCacheHandler(w http.ResponseWriter, r *http.Request) {
	pathArr := strings.Split(r.URL.Path, "/")[1:]
	fName := "./"
	if len(pathArr) < 3 {
		fName = fName + pathArr[1]
	} else {
		fName = fName + pathArr[2]
	}

	invalidAccess := false
	if c, err := r.Cookie("user"); err == http.ErrNoCookie {
		invalidAccess = true
	} else {
		cd := getCookieDetail(c.Value)
		if !cd.IsAdmin {
			for _, p := range cd.APaths {
				if p == fName[2:] { //todo check that `fName[2:]` is correct
					invalidAccess = true
				}
			}
		}
	}

	if _, err := os.Stat(fName); err == nil { // if file exists
		fReader, _ := os.Open(fName)
		defer fReader.Close()
		http.ServeContent(w, r, fName, time.Time{}, fReader)
		if len(pathArr) < 3 || pathArr[1] == "1" {
			go imageCacheClearer(fName)
		}
	}
	if invalidAccess {
		show404(w, r)
	}
}

func imagePreview(w http.ResponseWriter, r *http.Request, imgCacheName string) {
	data := imagePreviewData{false, false, "", ""}
	if imgCacheName == "0" { // insert does not exist
		data.FileUID = "file does not exist"
	} else if imgCacheName == "1" { // bad insert extension
		data.FileUID = "file has wrong extension"
	} else if imgCacheName == "2" { // too large of file
		data.FileUID = "file size is too large"
	} else { // image successfully found
		data.Success = true
		data.FileUID = imgCacheName[:strings.LastIndex(imgCacheName, ".")]
		data.FileExt = imgCacheName[strings.LastIndex(imgCacheName, "."):]
	}

	//t := template.Must(template.ParseFiles("./imagePreview.html"))
	t, _ := template.ParseFiles("./imagePreview.html") //todo test against template.Must()

	w.WriteHeader(http.StatusOK)
	if err := t.Execute(w, data); err != nil {
		fmt.Println(err)
		return
	}
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		fReader, _ := os.Open("./upload.html")
		defer fReader.Close()
		http.ServeContent(w, r, "./upload.html", time.Time{}, fReader)
	} else {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			imagePreview(w, r, "2")
			return
		}
		//todo handle file name
		//todo handle no file uploaded
		in, _, err := r.FormFile("fileUpload")
		if err != nil {
			fmt.Println(err)
			return
		}
		//defer in.Close()
		//fmt.Fprintf(w, "%v", handler.Header)
		//out, _ := os.OpenFile("./test/"+handler.Filename, os.O_WRONLY|os.O_CREATE, 0666)
		fName := string(getTimeHash()) + ".png" //todo check for proper extension
		out, _ := os.Create("./" + fName)
		//defer out.Close()
		io.Copy(out, in)
		in.Close()
		out.Close()
		c, err := r.Cookie("user")
		if err == http.ErrNoCookie {
			*c = createCookie(false)
			http.SetCookie(w, c)
		}
		appendPathToCookie(c.Value, fName)
		imagePreview(w, r, fName)
	}
}

func getImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		fReader, _ := os.Open("./getImage.html")
		defer fReader.Close()
		http.ServeContent(w, r, "getImage.html", time.Time{}, fReader)
	} else {
		u := r.URL.Query()
		path := u.Get("path")

		if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") {
			extension := path[strings.LastIndex(path, "."):]
			fName := string(getTimeHash()) + extension
			fileCreated := false
			if strings.HasPrefix(path, "127.0.0.1") {
				path = "./images" + path[9:]
				if _, err := os.Stat(path); err == nil {
					in, _ := os.Open(path)
					out, _ := os.Create("." + fName)
					defer in.Close()
					defer out.Close()
					io.Copy(out, in)
					fileCreated = true
				}
			} else {
				out, _ := os.Create("." + fName)
				defer out.Close()
				in, _ := http.Get(path)
				defer in.Body.Close()
				io.Copy(out, in.Body)
				fileCreated = true
			}
			if fileCreated {
				c, err := r.Cookie("user")
				if err == http.ErrNoCookie {
					*c = createCookie(false)
					http.SetCookie(w, c)
				}
				appendPathToCookie(c.Value, fName)

				imagePreview(w, r, fName)
			} else { // file does not exist
				imagePreview(w, r, "0")
			}
		} else { // bad file extension
			imagePreview(w, r, "1")
		}
	}
}

// Image Handling ----------------------
// -----------------------------------------------
// Generic Functions/Handlers ----------

func httpPathHandler(w http.ResponseWriter, r *http.Request) {
	fName := strings.Replace(r.URL.Path, "..", "", -1) //todo: check if needed

	if fName == "/" {
		fName = "/index.html"
	}
	if !strings.ContainsRune(fName, '.') {
		fName = fName + ".html" // lazy guessing
	}
	fName = "." + fName

	if _, err := os.Stat(fName); err == nil {
		fReader, _ := os.Open(fName)
		defer fReader.Close()
		if _, err := r.Cookie("user"); err == http.ErrNoCookie {
			c := createCookie(false)
			http.SetCookie(w, &c)
		}
		http.ServeContent(w, r, fName, time.Time{}, fReader)
	} else {
		show404(w, r)
	}
}

// Generic Functions/Handlers ----------
// -----------------------------------------------

func server() {
	// creating redis client
	rClient = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	// creating admin session
	setCookieDetail(cookieDetail{Cookie: http.Cookie{
		Name:     "user",
		Value:    "0",
		Expires:  time.Now().Add(100 * time.Hour),
		Secure:   true,
		HttpOnly: true,
	},
		IsAdmin: true,
	})

	http.HandleFunc("/", httpPathHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/getImage", getImageHandler)
	http.HandleFunc("/image_cache/", imageCacheHandler)
	http.HandleFunc("/imagePreview", show404)
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func Main() {
	server()
}
