package ui

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sentoris-ai/sentoris-proxy/internal/ui/api"
)

type Server struct {
	apiClient *api.Client
	staticDir string
	proxyBaseURL string
}

func NewServer(apiClient *api.Client, staticDir string, proxyBaseURL string) *Server {
	return &Server{
		apiClient: apiClient,
		staticDir: staticDir,
		proxyBaseURL: proxyBaseURL,
	}
}

func (s *Server) StaticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		if strings.Contains(path, "..") {
			http.NotFound(w, r)
			return
		}

		filePath := filepath.Join(s.staticDir, filepath.Clean(path))

		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			filePath = filepath.Join(s.staticDir, "index.html")
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("Failed to read file %s: %v", filePath, err)
			http.NotFound(w, r)
			return
		}

		contentType := getContentType(filePath)
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	})
}

func (s *Server) ProxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyURL := s.proxyBaseURL + r.URL.Path
		if r.URL.RawQuery != "" {
			proxyURL += "?" + r.URL.RawQuery
		}

		proxyReq, err := http.NewRequest(r.Method, proxyURL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for key, values := range r.Header {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}

		client := &http.Client{}
		resp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)

		buf := make([]byte, 8192)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	})
}

func getContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	default:
		return "text/plain; charset=utf-8"
	}
}
