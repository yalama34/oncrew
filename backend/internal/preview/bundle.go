package preview

import (
	"errors"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	MaxFiles     = 128
	MaxBytes     = 8 << 20
	MaxJSONBytes = 6*MaxBytes + 512<<10
)

var originHost = regexp.MustCompile(`^[a-z0-9-]+(?:\.[a-z0-9-]+)*$`)

var (
	ErrNoEntry   = errors.New("в артефактах нет index.html")
	ErrBadBundle = errors.New("в артефактах есть недопустимый путь или дубликат файла")
	ErrTooLarge  = errors.New("превью поддерживает до 128 файлов общим размером до 8 МБ")
)

type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func ContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	default:
		return ""
	}
}

func validPath(name string) bool {
	if name == "." || len(name) > 512 || !fs.ValidPath(name) || strings.ContainsAny(name, "\\:%?#") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func Bundle(files []File) ([]File, error) {
	bundle := make([]File, 0)
	seen := make(map[string]bool)
	size := 0
	for _, file := range files {
		if ContentType(file.Path) == "" {
			continue
		}
		if !validPath(file.Path) || seen[file.Path] {
			return nil, ErrBadBundle
		}
		seen[file.Path] = true
		bundle = append(bundle, file)
		size += len(file.Content)
		if len(bundle) > MaxFiles || size > MaxBytes {
			return nil, ErrTooLarge
		}
	}
	if !seen["index.html"] {
		return nil, ErrNoEntry
	}
	return bundle, nil
}

func ParseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !originHost.MatchString(u.Hostname()) ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.String() != raw {
		return nil, errors.New("expected an HTTP(S) origin without path, credentials or query")
	}
	if u.Host != u.Hostname() {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != u.Port() {
			return nil, errors.New("invalid origin port")
		}
	}
	return u, nil
}
