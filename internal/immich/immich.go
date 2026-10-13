// Package immich is a small client for the parts of the Immich API the
// import needs: duplicate check, upload, search, albums and stacks.
package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	BaseURL string // without /api
	Key     string
	HTTP    *http.Client
}

func New(baseURL, key string) *Client {
	// No overall timeout: large videos take long. Cancelling goes through ctx.
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, HTTP: &http.Client{}}
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	b := e.Body
	if len(b) > 300 {
		b = b[:300] + "…"
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, b)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// About checks reachability and API key and returns the Immich version.
func (c *Client) About(ctx context.Context) (string, error) {
	var r struct {
		Version string `json:"version"`
	}
	err := c.do(ctx, http.MethodGet, "/server/about", nil, &r)
	return r.Version, err
}

// --- Duplicate check --------------------------------------------------------

type CheckItem struct {
	ID       string `json:"id"`
	Checksum string `json:"checksum"` // SHA1, hex
}

type CheckResult struct {
	ID        string `json:"id"`
	Action    string `json:"action"` // accept or reject
	Reason    string `json:"reason"` // duplicate or unsupported-format
	AssetID   string `json:"assetId"`
	IsTrashed bool   `json:"isTrashed"`
}

func (c *Client) BulkCheck(ctx context.Context, items []CheckItem) ([]CheckResult, error) {
	var r struct {
		Results []CheckResult `json:"results"`
	}
	err := c.do(ctx, http.MethodPost, "/assets/bulk-upload-check", map[string]any{"assets": items}, &r)
	return r.Results, err
}

// --- Upload -----------------------------------------------------------------

type UploadResult struct {
	ID     string `json:"id"`
	Status string `json:"status"` // created or duplicate
}

// Upload sends a file without loading it into memory as a whole.
// progress is called with the number of bytes read each time.
func (c *Client) Upload(ctx context.Context, path, checksum string, progress func(int64)) (UploadResult, error) {
	var res UploadResult
	f, err := os.Open(path)
	if err != nil {
		return res, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return res, err
	}
	mtime := st.ModTime().UTC().Format("2006-01-02T15:04:05.000Z")

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			for k, v := range map[string]string{
				"fileCreatedAt": mtime, "fileModifiedAt": mtime, "filename": filepath.Base(path),
			} {
				if err := mw.WriteField(k, v); err != nil {
					return err
				}
			}
			part, err := mw.CreateFormFile("assetData", filepath.Base(path))
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, &countingReader{r: f, fn: progress}); err != nil {
				return err
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/assets", pr)
	if err != nil {
		pr.Close()
		return res, err
	}
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if checksum != "" {
		req.Header.Set("x-immich-checksum", checksum)
	}
	err = c.send(req, &res)
	pr.Close()
	return res, err
}

type countingReader struct {
	r  io.Reader
	fn func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.fn != nil {
		c.fn(int64(n))
	}
	return n, err
}

// --- Search -----------------------------------------------------------------

type Asset struct {
	ID               string    `json:"id"`
	Type             string    `json:"type"`
	OriginalFileName string    `json:"originalFileName"`
	IsTrashed        bool      `json:"isTrashed"`
	CreatedAt        time.Time `json:"createdAt"`
	ExifInfo         *struct {
		DateTimeOriginal *time.Time `json:"dateTimeOriginal"`
		Make             string     `json:"make"`
		Model            string     `json:"model"`
	} `json:"exifInfo"`
	Stack *struct {
		ID string `json:"id"`
	} `json:"stack"`
}

type SearchQuery struct {
	CreatedAfter *time.Time `json:"createdAfter,omitempty"`
	WithExif     bool       `json:"withExif,omitempty"`
	WithStacked  bool       `json:"withStacked,omitempty"`
	Size         int        `json:"size,omitempty"`
	Page         int        `json:"page,omitempty"`
	Cursor       string     `json:"cursor,omitempty"`
}

// Search pages through all hits and calls fn for each page.
// From Immich 3.2 on by cursor, before that by page number.
func (c *Client) Search(ctx context.Context, q SearchQuery, fn func([]Asset) error) error {
	if q.Size == 0 {
		q.Size = 1000
	}
	seen := map[string]bool{}
	for {
		var r struct {
			Assets struct {
				Items      []Asset `json:"items"`
				NextPage   *string `json:"nextPage"`
				NextCursor *string `json:"nextCursor"`
			} `json:"assets"`
		}
		if err := c.do(ctx, http.MethodPost, "/search/metadata", q, &r); err != nil {
			return err
		}
		fresh := r.Assets.Items[:0]
		for _, a := range r.Assets.Items {
			if !seen[a.ID] {
				seen[a.ID] = true
				fresh = append(fresh, a)
			}
		}
		// If Immich only returns what was already read, paging is broken:
		// stop instead of fetching the same page forever
		if len(fresh) == 0 && len(r.Assets.Items) > 0 {
			return fmt.Errorf("paging failed: Immich returns the same files again")
		}
		if err := fn(fresh); err != nil {
			return err
		}
		switch {
		case r.Assets.NextCursor != nil && *r.Assets.NextCursor != "":
			q.Cursor = *r.Assets.NextCursor
		case r.Assets.NextPage != nil && *r.Assets.NextPage != "":
			fmt.Sscan(*r.Assets.NextPage, &q.Page)
		default:
			return nil
		}
	}
}

// --- Albums and stacks ------------------------------------------------------

// AddToAlbum puts the files into the album with this name and creates it if
// it doesn't exist yet.
func (c *Client) AddToAlbum(ctx context.Context, name string, ids []string) (created bool, err error) {
	var albums []struct {
		ID        string `json:"id"`
		AlbumName string `json:"albumName"`
	}
	if err := c.do(ctx, http.MethodGet, "/albums", nil, &albums); err != nil {
		return false, fmt.Errorf("read albums: %w", err)
	}
	albumID := ""
	for _, a := range albums {
		if a.AlbumName == name {
			albumID = a.ID
			break
		}
	}
	if albumID == "" {
		var a struct {
			ID string `json:"id"`
		}
		if err := c.do(ctx, http.MethodPost, "/albums", map[string]any{"albumName": name}, &a); err != nil {
			return false, fmt.Errorf("create album: %w", err)
		}
		albumID, created = a.ID, true
	}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		if err := c.do(ctx, http.MethodPut, "/albums/"+albumID+"/assets", map[string]any{"ids": chunk}, nil); err != nil {
			return created, fmt.Errorf("add to album: %w", err)
		}
	}
	return created, nil
}

// CreateStack stacks the files; the first one is on top.
func (c *Client) CreateStack(ctx context.Context, ids []string) error {
	return c.do(ctx, http.MethodPost, "/stacks", map[string]any{"assetIds": ids}, nil)
}
