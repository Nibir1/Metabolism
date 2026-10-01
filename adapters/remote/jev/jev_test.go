package jev

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	typesafe "github.com/nibir1/typesafe-go"
	"github.com/nibir1/typesafe-go/typesafetest"

	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func ep(i int) types.Episode {
	at := t0.Add(time.Duration(i) * time.Second)
	return types.Episode{ID: types.NewID(at), Request: types.Request{Text: "build the project"}, End: types.RunFailed,
		Steps: []types.Step{{Tool: "file", Action: "read", Subject: "package.json", OK: false, Err: "no such file"}}}
}

var (
	with    = types.ReplayResult{Steps: []types.Step{{Tool: "file", Action: "read", Subject: "go.mod", OK: true}}}
	without = types.ReplayResult{Steps: []types.Step{{Tool: "file", Action: "read", Subject: "package.json", OK: true}}}
)

// Jev picks the with-lesson plan whichever position it was given in, and its
// probability becomes the verdict's confidence.
func TestJevVerdictMapsThroughThePair(t *testing.T) {
	for i := range 8 {
		e := ep(i)
		pair := judge.NewPair(e, nil, with, without)
		choice := "b"
		if pair.WithIsA {
			choice = "a"
		}
		probs := map[string]float64{"a": 0.1, "b": 0.1, "same": 0.1}
		probs[choice] = 0.8
		mock := typesafetest.NewMock().On(QuestionID, typesafetest.Choice(probs))

		v, err := Judge{API: mock}.Compare(context.Background(), e, nil, with, without)
		if err != nil {
			t.Fatal(err)
		}
		if v.Prefer != types.PreferWith || v.Confidence < 0.79 || v.Confidence > 0.81 {
			t.Fatalf("episode %d: verdict %+v", i, v)
		}

		calls := mock.Calls()
		if len(calls) != 1 {
			t.Fatalf("%d calls", len(calls))
		}
		q, ok := calls[0].Questions[QuestionID].(typesafe.Choice)
		if !ok || len(q.Criteria) != 3 {
			t.Fatalf("question sent: %+v", calls[0].Questions)
		}
		state, _ := json.Marshal(calls[0].State)
		var sent judge.Pair
		if err := json.Unmarshal(state, &sent); err != nil || sent.Request != "build the project" || len(sent.PlanA) == 0 {
			t.Fatalf("state sent: %s", state)
		}
	}
}

func TestJevSameIsNeither(t *testing.T) {
	mock := typesafetest.NewMock().On(QuestionID, typesafetest.Choice(map[string]float64{"a": 0.2, "b": 0.2, "same": 0.6}))
	v, err := Judge{API: mock}.Compare(context.Background(), ep(1), nil, with, without)
	if err != nil {
		t.Fatal(err)
	}
	if v.Prefer != types.PreferNeither {
		t.Fatalf("verdict %+v", v)
	}
}

func TestJevErrorsPropagate(t *testing.T) {
	mock := typesafetest.NewMock().ReturnError(typesafe.ErrNoAPIKey)
	if _, err := (Judge{API: mock}).Compare(context.Background(), ep(1), nil, with, without); err == nil {
		t.Fatal("an API error was swallowed")
	}
	if (Judge{}).Name() != "jev" || (Judge{Model: "jev-1.13"}).Name() != "jev/jev-1.13" {
		t.Fatal("names")
	}
}
