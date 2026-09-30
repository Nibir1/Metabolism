package bench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/Nibir1/metabolism/types"
)

// Point is one window of a learning curve.
type Point struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Metrics Metrics   `json:"metrics"`
}

// Curve is a host's metrics over consecutive time windows. It is the shape of
// every claim Metabolism makes: better than the baseline, window by window.
type Curve struct {
	Label  string        `json:"label"`
	Window time.Duration `json:"window"`
	Total  Metrics       `json:"total"`
	Points []Point       `json:"points"`
}

// BuildCurve buckets ds into consecutive windows starting at the first
// episode, by episode start time. Empty windows are kept, so a gap in use
// shows as a gap rather than silently joining its neighbours.
func BuildCurve(label string, ds Dataset, window time.Duration) (Curve, error) {
	if window <= 0 {
		return Curve{}, fmt.Errorf("window must be positive, got %s", window)
	}
	c := Curve{Label: label, Window: window, Total: Compute(ds.Episodes, ds.Outcomes)}
	if len(ds.Episodes) == 0 {
		return c, nil
	}
	eps := slices.Clone(ds.Episodes)
	slices.SortStableFunc(eps, func(a, b types.Episode) int { return a.StartedAt.Compare(b.StartedAt) })

	start := eps[0].StartedAt.Truncate(window)
	i := 0
	for ws := start; i < len(eps); ws = ws.Add(window) {
		we := ws.Add(window)
		j := i
		for j < len(eps) && eps[j].StartedAt.Before(we) {
			j++
		}
		c.Points = append(c.Points, Point{Start: ws, End: we, Metrics: Compute(eps[i:j], ds.Outcomes)})
		i = j
	}
	return c, nil
}

// WriteJSON writes c as indented JSON.
func (c Curve) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

// csvHeader is the column order of WriteCSV.
var csvHeader = []string{
	"start", "end", "episodes", "finished", "finish_rate",
	"model_calls_per_finished", "est_tokens_per_finished",
	"budget_exhaustion_rate", "failed_step_rate",
	"undo_rate", "decline_rate", "repeat_rate",
}

// WriteCSV writes one row per window, for spreadsheets and plotting tools.
func (c Curve) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
	for _, p := range c.Points {
		m := p.Metrics
		row := []string{
			p.Start.UTC().Format(time.RFC3339), p.End.UTC().Format(time.RFC3339),
			strconv.Itoa(m.Episodes), strconv.Itoa(m.Finished), f(m.FinishRate),
			f(m.ModelCallsPerFinished), f(m.EstTokensPerFinished),
			f(m.BudgetExhaustionRate), f(m.FailedStepRate),
			f(m.UndoRate), f(m.DeclineRate), f(m.RepeatRate),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
