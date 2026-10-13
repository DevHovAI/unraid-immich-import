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

// Run reads the library (or only what was uploaded since since), forms pairs
// and creates the stacks. progress receives updates for /status.
func Run(ctx context.Context, c *immich.Client, since *time.Time, tol time.Duration,
	cover string, dry bool, progress func(string)) (Result, error) {
	var res Result
	col := NewCollector()
	q := immich.SearchQuery{CreatedAfter: since, WithExif: true, WithStacked: true}
	err := c.Search(ctx, q, func(page []immich.Asset) error {
		col.Add(page)
		progress(fmt.Sprintf("reading library: %d files", col.Seen))
		return nil
	})
	if err != nil {
		return res, fmt.Errorf("search: %w", err)
	}
	progress(fmt.Sprintf("looking for pairs in %d files …", col.Seen))
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
			line := fmt.Sprintf("creating stacks: %d of %d (%d %%)", n, len(pairs), 100*n/len(pairs))
			if n >= 20 {
				rest := time.Duration(float64(time.Since(t0)) / float64(n) * float64(len(pairs)-n))
				if rest < time.Minute {
					line += ", almost done"
				} else {
					line += fmt.Sprintf(", about %d min left", int(rest.Round(time.Minute).Minutes()))
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
