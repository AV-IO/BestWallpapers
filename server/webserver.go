package main

import (
	"encoding/binary"
	"encoding/hex"
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

func serveFile(fName string, w http.ResponseWriter, r *http.Request) error {
	if _, err := os.Stat(fName); err == nil {
		fReader, _ := os.Open(fName)
		defer fReader.Close()
		http.ServeContent(w, r, fName, time.Time{}, fReader)
		return nil
	} else {
		return err
	}
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

func getCookieDetail(cName string) (cookieDetail, error) {
	j, err := rClient.Get(cName).Result()
	if err != nil {
		return cookieDetail{}, http.ErrNoCookie
	}
	var newCD cookieDetail
	json.Unmarshal([]byte(j), &newCD)
	return newCD, nil
}

func appendPathToCookie(cName string, newPath string) {
	cd, _ := getCookieDetail(cName)
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
		Value:    hex.EncodeToString(t),
		Expires:  time.Now().Add(time.Hour),
		Secure:   false, //todo add secure back when adding https
		HttpOnly: true,
	}
	setCookieDetail(cookieDetail{Cookie: c, IsAdmin: isAdmin})
	return
}

func checkCookie(name string, w *http.ResponseWriter, r *http.Request) (cookieDetail, error) {
	cookieSlice := r.Cookies()
	var i int
	var c *http.Cookie

	for i, c = range cookieSlice {
		if c.Name == name {
			cd, err := getCookieDetail(cookieSlice[i].Value)
			if err != http.ErrNoCookie {
				return cd, nil
			}
			break
		}
	}

	cd, _ := getCookieDetail(createCookie(false).Value)
	http.SetCookie(*w, &cd.Cookie)
	return cd, http.ErrNoCookie
}

// Session Handling --------------------
// -----------------------------------------------
// Image Handling ----------------------

type imagePreviewData struct {
	// used within HTML template
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
	fName := "./image_cache/"
	if len(pathArr) < 3 {
		fName = fName + pathArr[1]
	} else if len(pathArr) == 3 {
		fName = fName + pathArr[2]
		if pathArr[1] == "1" {
			go imageCacheClearer(fName)
		}
		return
	}

	invalidAccess := true
	cd, err := checkCookie("user", &w, r)
	if err != http.ErrNoCookie {
		if !cd.IsAdmin {
			iter := 0
			p := ""
			for iter, p = range cd.APaths {
				if p == fName[2:] {
					invalidAccess = false
					break
				}
			}
			if !invalidAccess {
				cd.APaths[iter] = cd.APaths[len(cd.APaths)-1]
				cd.APaths[len(cd.APaths)-1] = ""
				cd.APaths = cd.APaths[:len(cd.APaths)-1]
				setCookieDetail(cd)
			}
		} else {
			invalidAccess = false
		}
	}

	if err := serveFile(fName, w, r); err != nil {
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

/*
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
		//todo+ remove file name
		//todo handle no file uploaded
		in, _, err := r.FormFile("fileUpload")
		if err != nil {
			fmt.Println(err)
			return
		}
		//defer in.Close()
		//fmt.Fprintf(w, "%v", handler.Header)
		//out, _ := os.OpenFile("./test/"+handler.Filename, os.O_WRONLY|os.O_CREATE, 0666)
		fName := hex.EncodeToString(getTimeHash()) + ".png" //todo check for proper extension
		out, _ := os.Create("./image_cache/" + fName)
		//defer out.Close()
		io.Copy(out, in)
		in.Close()
		out.Close()
		cd, _ := checkCookie("user", &w, r)
		appendPathToCookie(cd.Cookie.Value, "image_cache/"+fName)
		imagePreview(w, r, fName)
	}
}
*/

func getImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		fReader, _ := os.Open("./getImage.html")
		defer fReader.Close()
		http.ServeContent(w, r, "getImage.html", time.Time{}, fReader)
	} else {
		path := r.FormValue("path")

		if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") {
			extension := path[strings.LastIndex(path, "."):]
			fName := hex.EncodeToString(getTimeHash()) + extension
			fileCreated := false
			if strings.HasPrefix(path, "127.0.0.1") {
				path = "./images" + path[9:]
				if _, err := os.Stat(path); err == nil {
					in, _ := os.Open(path)
					out, _ := os.Create("./image_cache/" + fName)
					defer in.Close()
					defer out.Close()
					io.Copy(out, in)
					fileCreated = true
				}
			} else {
				out, _ := os.Create("./image_cache/" + fName)
				defer out.Close()
				in, _ := http.Get(path)
				defer in.Body.Close()
				io.Copy(out, in.Body)
				fileCreated = true
			}
			if fileCreated {
				cd, _ := checkCookie("user", &w, r)
				appendPathToCookie(cd.Cookie.Value, "image_cache/"+fName)
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
// Admin Page Handler ------------------

func reviewImages(w http.ResponseWriter, r *http.Request) {
	cd, err := checkCookie("user", &w, r)
	if err != http.ErrNoCookie && cd.IsAdmin {
		if r.Method == "GET" {
			if err := serveFile("./reviewImages.html", w, r); err == nil {
				return
			}
		} else {
			if err := serveFile("../reviewFlag.txt", w, r); err == nil {
				return
			}
		}
	}
	show404(w, r)
}

// Admin Page Handler ------------------
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

	checkCookie("user", &w, r)
	if err := serveFile(fName, w, r); err != nil {
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
	defer rClient.Close()
	// creating admin session
	setCookieDetail(cookieDetail{Cookie: http.Cookie{
		Name:     "user",
		Value:    "0147532698",
		Expires:  time.Now().Add(100 * time.Hour),
		Secure:   false, //todo add secure back when adding https
		HttpOnly: true,
	},
		IsAdmin: true,
	})

	http.HandleFunc("/", httpPathHandler)
	http.HandleFunc("/upload", show404)
	http.HandleFunc("/getImage", getImageHandler)
	http.HandleFunc("/image_cache/", imageCacheHandler)
	http.HandleFunc("/imagePreview", show404)
	http.HandleFunc("/reviewImages", reviewImages)
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func main() {
	server()
}
