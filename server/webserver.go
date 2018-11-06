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
	"regexp"
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
	Submission bool
	Success    bool
	IsAdmin    bool
	FileUID    string
	FileExt    string
}

func imageCacheClearer(fName string) {
	time.Sleep(5 * time.Second)
	os.Remove(fName)
}

func imageCacheHandler(w http.ResponseWriter, r *http.Request) {
	pathArr := strings.Split(r.URL.Path, "/")[1:]
	fName := "./image_cache/"
	cd, err := checkCookie("user", &w, r)

	if len(pathArr) < 3 {
		fName = fName + pathArr[1]
	} else if len(pathArr) == 3 {
		fName = fName + pathArr[2]
		if pathArr[1] == "1" {
			go imageCacheClearer(fName)
		}
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
		return
	}

	invalidAccess := true
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

func imagePreview(w http.ResponseWriter, r *http.Request, imgCacheName string, submission bool) {
	data := imagePreviewData{submission, false, false, "", ""}

	if imgCacheName == "0" { // insert does not exist
		data.FileUID = "file does not exist"
	} else if imgCacheName == "1" { // bad insert extension
		data.FileUID = "file has wrong extension"
	} else if imgCacheName == "2" { // too large of file
		data.FileUID = "we already have that image"
	} else if imgCacheName == "3" { // bad insert extension
		data.FileUID = "please specify a valid URL"
	} else { // image successfully found
		data.Success = true
		data.FileUID = imgCacheName[:strings.LastIndex(imgCacheName, ".")]
		data.FileExt = imgCacheName[strings.LastIndex(imgCacheName, "."):]
	}

	t, _ := template.ParseFiles("./imagePreview.html")

	w.WriteHeader(http.StatusOK)
	if err := t.Execute(w, data); err != nil {
		fmt.Println(err)
		return
	}
}

func getImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		fReader, _ := os.Open("./getImage.html")
		defer fReader.Close()
		http.ServeContent(w, r, "getImage.html", time.Time{}, fReader)
	} else {
		path := r.FormValue("path")
		if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") {
			if strings.HasPrefix(path, "http://wallpapers.mysterious-hashes.net") {
				imagePreview(w, r, "2", true)
				return
			}
			extension := path[strings.LastIndex(path, "."):]
			s := path[:strings.LastIndex(path, extension)]
			m, _ := regexp.MatchString(`http(s)?:\/\/[\w.-]+(?:\.[\w\.-]+)+[\w\-\._~:/?#\[\]@!\$&'\(\)\*\+,;=.]+`, s)
			if m {
				fName := hex.EncodeToString(getTimeHash()) + extension
				fileCreated := false
				if strings.HasPrefix(path, "http://127.0.0.1") {
					path = "./images" + path[16:]
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
					imagePreview(w, r, fName, true)
				} else { // file does not exist
					imagePreview(w, r, "0", true)
				}
			} else {
				imagePreview(w, r, "3", true)
			}
		} else { // bad file extension
			imagePreview(w, r, "1", true)
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

func imageViewer(w http.ResponseWriter, r *http.Request) {
	fName := strings.Replace(r.URL.Path, "..", "", -1) //todo: check if needed
	fName = "/images" + fName[10:]
	checkCookie("user", &w, r)
	imagePreview(w, r, fName, false)
}

// Generic Functions/Handlers ----------
// -----------------------------------------------

func server() {
	// creating redis client
	rClient = redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "",
		DB:       0,
	})
	defer rClient.Close()
	// creating admin session
	setCookieDetail(cookieDetail{Cookie: http.Cookie{
		Name:     "user",
		Value:    "52d53a75e67a3ece53dbb15ca4122616714906c7706ef9774f4d441daf2c7fb5ae4848b88487218304247850a3c2ef53dbbcd81330596543aec33340875fbb31",
		Expires:  time.Now().Add(100 * time.Hour),
		Secure:   false, //todo add secure back when adding https
		HttpOnly: true,
	},
		IsAdmin: true,
	})

	http.HandleFunc("/", httpPathHandler)
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
