package main

import (
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
	"golang.org/x/crypto/sha3"
	"io"
	"html/template"
	"fmt"
)

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
		http.ServeContent(w, r, fName, time.Time{}, fReader)
	} else { // show 404 page
		show404(w, r)
	}
}

func imageCacheHandler(w http.ResponseWriter, r *http.Request) {
	fName := "." + r.URL.Path

	if _, err := os.Stat(fName); err == nil {
		fReader, _ := os.Open(fName)
		defer fReader.Close()
		defer os.Remove(fName)
		http.ServeContent(w, r, fName, time.Time{}, fReader)
	} else { // show 404 page
		show404(w, r)
	}
}

func imagePreview(w http.ResponseWriter, r *http.Request, imgCacheName string) {
	var insert string

	if imgCacheName == "0" { // file does not exist
		insert = "<p>file does not exist</p>"
	} else if imgCacheName == "1" { // bad file extension
		insert = "<p>file has wrong extension</p>"
	} else { // image successfully found
		insert = "<img src=" + imgCacheName + ">" //todo: set width/height
	}

	fBytes, _ := ioutil.ReadFile("./imagePreview.html") //todo: create imagePreview.html
	fString := string(fBytes)
	strings.Replace(fString, "", insert, 1)

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(fString))
}

func uploadHandler(w http.ResponseWriter, r *http.Request) { // todo: check https://zupzup.org/go-http-file-upload-download/
	if r.Method == "GET" { //todo: wtf is this doing?
		buf := []byte(time.Now().String())
		h := make([]byte, 64)
		sha3.ShakeSum128(h, buf)

		t, _ := template.ParseFiles("upload.gtpl")
		t.Execute(w, h)
	} else { //todo: test if there is a better method
		r.ParseMultipartForm(32 << 20)
		in, handler, _ := r.FormFile("uploadfile")
		defer in.Close()
		fmt.Fprintf(w, "%v", handler.Header)
		out, _ := os.OpenFile("./test/"+handler.Filename, os.O_WRONLY|os.O_CREATE, 0666)
		defer out.Close()
		io.Copy(out, in)
	}
}

func getImageHandler(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query()
	path := u.Get("path")

	if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") {
		extension := path[strings.LastIndex(path, "."):]
		imgCacheName := "/image_cache/" + string(getTimeHash()) + extension
		fileCreated := false
		if strings.HasPrefix(path,"127.0.0.1") {
			path = "./images" + path[9:]
			if _, err := os.Stat(path); err == nil {
				in, _ := os.Open(path)
				defer in.Close()
				out, _ := os.Create("." + imgCacheName)
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
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func main() {
	server()
}
