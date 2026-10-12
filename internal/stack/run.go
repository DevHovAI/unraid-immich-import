package stack

import (
	"context"
	"fmt"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/immich"
)

type Result struct {
	Pairs, Created, Ambiguous, NoDate, Errors int
	ErrorSamples                              []string
}

// Run liest die Bibliothek (oder nur seit since Hochgeladenes), bildet Paare
// und legt die Stapel an. progress bekommt Zwischenstände für /status.
func Run(ctx context.Context, c *immich.Client, since *time.Time, tol time.Duration,
	cover string, dry bool, progress func(string)) (Result, error) {
	var res Result
	col := NewCollector()
	q := immich.SearchQuery{CreatedAfter: since, WithExif: true, WithStacked: true}
	err := c.Search(ctx, q, func(page []immich.Asset) error {
		col.Add(page)
		progress(fmt.Sprintf("Bibliothek lesen: %d Dateien", col.Seen))
		return nil
	})
	if err != nil {
		return res, fmt.Errorf("Suche: %w", err)
	}
	progress(fmt.Sprintf("Paare suchen in %d Dateien …", col.Seen))
	pairs, st := col.Pairs(tol, cover)
	res.Pairs, res.Ambiguous, res.NoDate = len(pairs), st.Ambiguous, st.NoDate
	if dry {
		return res, nil
	}

	t0 := time.Now()
	for n, p := range pairs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if n%10 == 0 {
			line := fmt.Sprintf("Stapel anlegen: %d von %d (%d %%)", n, len(pairs), 100*n/len(pairs))
			if n >= 20 {
				rest := time.Duration(float64(time.Since(t0)) / float64(n) * float64(len(pairs)-n))
				if rest < time.Minute {
					line += ", gleich fertig"
				} else {
					line += fmt.Sprintf(", noch ca. %d Min.", int(rest.Round(time.Minute).Minutes()))
				}
			}
			progress(line)
		}
		if err := c.CreateStack(ctx, []string{p.Primary, p.Other}); err != nil {
			res.Errors++
			if len(res.ErrorSamples) < 3 {
				res.ErrorSamples = append(res.ErrorSamples, fmt.Sprintf("%s: %v", p.PrimaryName, err))
			}
			continue
		}
		res.Created++
	}
	return res, nil
}
