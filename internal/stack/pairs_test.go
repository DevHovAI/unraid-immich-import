package stack

import (
	"testing"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/immich"
)

func asset(id, name string, taken *time.Time, model string, stacked bool) immich.Asset {
	a := immich.Asset{ID: id, Type: "IMAGE", OriginalFileName: name}
	a.ExifInfo = &struct {
		DateTimeOriginal *time.Time `json:"dateTimeOriginal"`
		Make             string     `json:"make"`
		Model            string     `json:"model"`
	}{DateTimeOriginal: taken, Make: "SONY", Model: model}
	if stacked {
		a.Stack = &struct {
			ID string `json:"id"`
		}{ID: "s"}
	}
	return a
}

func at(sec int) *time.Time {
	t := time.Date(2026, 10, 1, 12, 0, sec, 0, time.UTC)
	return &t
}

func TestPairs(t *testing.T) {
	c := NewCollector()
	c.Add([]immich.Asset{
		// normales Paar
		asset("r1", "DSC00001.ARW", at(0), "ILCE-7M4", false),
		asset("h1", "DSC00001.HIF", at(1), "ILCE-7M4", false),
		// zu weit auseinander
		asset("r2", "DSC00002.ARW", at(0), "ILCE-7M4", false),
		asset("h2", "DSC00002.HIF", at(10), "ILCE-7M4", false),
		// andere Kamera, gleicher Name
		asset("r3", "DSC00003.ARW", at(0), "ILCE-7M4", false),
		asset("h3", "DSC00003.HIF", at(0), "ILCE-6400", false),
		// mehrdeutig: HIF und JPG zum selben RAW
		asset("r4", "DSC00004.ARW", at(0), "ILCE-7M4", false),
		asset("h4", "DSC00004.HIF", at(0), "ILCE-7M4", false),
		asset("j4", "DSC00004.JPG", at(0), "ILCE-7M4", false),
		// schon gestapelt
		asset("r5", "DSC00005.ARW", at(0), "ILCE-7M4", true),
		asset("h5", "DSC00005.HIF", at(0), "ILCE-7M4", true),
		// noch ohne Aufnahmedatum
		asset("r6", "DSC00006.ARW", nil, "", false),
		asset("h6", "dsc00006.hif", at(0), "ILCE-7M4", false),
	})
	pairs, st := c.Pairs(2*time.Second, "display")
	if len(pairs) != 1 || pairs[0].Primary != "h1" || pairs[0].Other != "r1" {
		t.Fatalf("Paare: %+v", pairs)
	}
	if st.Ambiguous != 1 || st.NoDate != 1 {
		t.Fatalf("Stats: %+v", st)
	}
	pairs, _ = c.Pairs(2*time.Second, "raw")
	if pairs[0].Primary != "r1" {
		t.Fatalf("cover raw: %+v", pairs)
	}
}

func TestIgnoresVideosAndTrash(t *testing.T) {
	c := NewCollector()
	v := asset("v", "C0001.MP4", at(0), "", false)
	v.Type = "VIDEO"
	tr := asset("h", "DSC00001.HIF", at(0), "", false)
	tr.IsTrashed = true
	c.Add([]immich.Asset{v, tr, asset("r", "DSC00001.ARW", at(0), "", false)})
	if pairs, _ := c.Pairs(time.Second, "display"); len(pairs) != 0 {
		t.Fatalf("%+v", pairs)
	}
}
