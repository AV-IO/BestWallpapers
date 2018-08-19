package main

import (
	"encoding/binary"
	"fmt"
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

type imagePreviewData struct {
	Success bool
	IsAdmin bool
	FileUID string
	FileExt string
}

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
			t := getTimeHash()
			binary.BigEndian.PutUint64(t[16:], binary.BigEndian.Uint64(t[16:])+rand.Uint64()) // adding random 64 bits to end of hash
			c := http.Cookie{
				Name:     "user",
				Value:    string(t),
				Expires:  time.Now().Add(time.Hour),
				Secure:   true,
				HttpOnly: true,
			}
			// todo put cookie in cookiejar
			http.SetCookie(w, &c) //todo reference cookie within cookiejar
		}
		http.ServeContent(w, r, fName, time.Time{}, fReader)
	} else { // show 404 page
		show404(w, r)
	}
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

	if _, err := os.Stat(fName); err == nil { // if file exists
		fReader, _ := os.Open(fName)
		defer fReader.Close()
		http.ServeContent(w, r, fName, time.Time{}, fReader)
		if len(pathArr) < 3 || pathArr[1] == "1" {
			go imageCacheClearer(fName)
		}
	} else {
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
	t, _ := template.ParseFiles("./imagePreview.html")

	w.WriteHeader(http.StatusOK)
	//w.Header().Set("Content-Type", "text/html")
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
		//todo handle no file uploaded
		in, _, err := r.FormFile("fileUpload")
		if err != nil {
			fmt.Println(err)
			return
		}
		//defer in.Close()
		fName := string(getTimeHash())
		//fmt.Fprintf(w, "%v", handler.Header)
		//out, _ := os.OpenFile("./test/"+handler.Filename, os.O_WRONLY|os.O_CREATE, 0666)
		out, _ := os.Create(fName)
		//defer out.Close()
		io.Copy(out, in)
		in.Close()
		out.Close()
		imagePreview(w, r, fName)
	}
}

func getImageHandler(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query()
	path := u.Get("path")

	if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") {
		extension := path[strings.LastIndex(path, "."):]
		imgCacheName := string(getTimeHash()) + extension
		fileCreated := false
		if strings.HasPrefix(path, "127.0.0.1") {
			path = "./images" + path[9:]
			if _, err := os.Stat(path); err == nil {
				in, _ := os.Open(path)
				out, _ := os.Create("." + imgCacheName)
				defer in.Close()
				defer out.Close()
				io.Copy(out, in)
				fileCreated = true
			}
		} else {
			out, _ := os.Create("." + imgCacheName)
			defer out.Close()
			in, _ := http.Get(path)
			defer in.Body.Close()
			io.Copy(out, in.Body)
			fileCreated = true
		}
		if fileCreated {
			imagePreview(w, r, imgCacheName)
		} else { // file does not exist
			imagePreview(w, r, "0")
		}
	} else { // bad file extension
		imagePreview(w, r, "1")
	}
}

func server() {
	http.HandleFunc("/", httpPathHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/getImage", getImageHandler)
	http.HandleFunc("/image_cache/", imageCacheHandler)
	http.HandleFunc("/imagePreview", show404)
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func main() {
	server()
}
