package main

import (
	"fmt"
	"golang.org/x/crypto/sha3"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
)

type imagePreviewData struct {
	Success bool
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
	data := imagePreviewData{false, "", ""}
	if imgCacheName == "0" { // insert does not exist
		data.FileUID = "file does not exist"
	} else if imgCacheName == "1" { // bad insert extension
		data.FileUID = "file has wrong extension"
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

func imgprevwrap(w http.ResponseWriter, r *http.Request) {
	imagePreview(w, r, "test.png")
}

func uploadHandler(w http.ResponseWriter, r *http.Request) { // todo: check https://zupzup.org/go-http-file-upload-download/
	if r.Method == "GET" {
		buf := []byte(time.Now().String())
		h := make([]byte, 64)
		sha3.ShakeSum128(h, buf)

		t, _ := template.ParseFiles("upload.gtpl")
		t.Execute(w, h)
	} else {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return //file too big
		}
		in, handler, err := r.FormFile("uploadfile")
		if err != nil {
			return
		}
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
		imgCacheName := string(getTimeHash()) + extension
		fileCreated := false
		if strings.HasPrefix(path, "127.0.0.1") {
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
	//http.HandleFunc("/imagePreview", show404)
	http.HandleFunc("/imagePreview", imgprevwrap)
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func main() {
	server()
}
