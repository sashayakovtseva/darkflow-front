package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

var inputDir string
var outputDir string
var darkflowURL string
var insecureClient *http.Client

func readFlags() {
	flag.StringVar(&inputDir, "input", "/input", "directory to store downloaded input images")
	flag.StringVar(&outputDir, "output", "/output", "directory to store processed input images")
	flag.StringVar(&darkflowURL, "darkflow-url", "http://darkflow:8000", "URL where darkflow is waiting")
	flag.Parse()
}

func main() {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	insecureClient = &http.Client{Transport: tr}

	readFlags()
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatal(err)
	}

	log.Printf("Starting file server at %s", outputDir)
	http.Handle("/output/", http.StripPrefix("/output/", http.FileServer(http.Dir(outputDir))))
	http.HandleFunc("/recognize", recognize)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

type recognizeRequest struct {
	ImageURLs []string `json:"image_urls"`
}

type darkflowRequest struct {
	InputDir  string `json:"input_dir"`
	OutputDir string `json:"output_dir"`
}

func setupResponse(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}

func recognize(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		setupResponse(w)
		return
	}
	var req recognizeRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || len(req.ImageURLs) == 0 {
		jsonError(w, http.StatusBadRequest, fmt.Errorf("invalid json body: %v", err))
		return
	}

	log.Printf("Got recognize request %+v", req)
	id := generateID(8)
	err = os.Mkdir(filepath.Join(inputDir, id), 0755)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Errorf("could not create input dir: %v", err))
		return
	}

	input := filepath.Join(inputDir, id)
	for i, img := range req.ImageURLs {
		err := wget(img, filepath.Join(input, fmt.Sprintf("%d.jpg", i)))
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err)
			return
		}
	}

	output := filepath.Join(outputDir, id)

	var buf bytes.Buffer
	err = json.NewEncoder(&buf).Encode(darkflowRequest{
		InputDir:  input,
		OutputDir: output,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Errorf("could not encode darkflow request: %v", err))
		return
	}

	resp, err := http.Post(darkflowURL, "application/json", &buf)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Errorf("could not call darkflow: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		jsonError(w, resp.StatusCode, fmt.Errorf("darkflow returned error"))
		return
	}

	files, err := ioutil.ReadDir(output)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Errorf("could not read output dir: %v", err))
		return
	}

	n := len(files)
	imgs := make([]string, n)
	for i := 0; i < n; i++ {
		imgs[i] = filepath.Join("/output", id, files[i].Name())
	}

	log.Printf("Sending recognize response: %+v", imgs)
	jsonResponse(w, http.StatusOK, imgs)
}

func wget(from, to string) error {
	response, err := insecureClient.Get(from)
	if err != nil {
		return fmt.Errorf("could not wget image: %v", err)
	}
	defer response.Body.Close()

	file, err := os.Create(to)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, response.Body)
	return err
}

func jsonError(w http.ResponseWriter, status int, err error) {
	jsonResponse(w, status, map[string]string{"reason": err.Error()})
}

func jsonResponse(w http.ResponseWriter, status int, payload interface{}) {
	setupResponse(w)
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
}

func generateID(len int) string {
	buf := make([]byte, (len-1)/2+1)
	rand.Read(buf)
	return hex.EncodeToString(buf)[:len]
}

