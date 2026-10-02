// Package jev is a nutrient-test judge backed by TypeSafe's Jev model, through
// the typesafe-go SDK (github.com/nibir1/typesafe-go).
//
// Jev answers typed questions with calibrated probabilities, which is exactly
// what a judge needs: the confidence is Jev's own probability for its choice,
// not a number a generative model was asked to make up. The question is a
// single atomic Choice, checked with the SDK's analyzers (typesafe-lint): one
// judgment per question, nothing from Jev's known jagged edges such as
// counting or dates. What "better" means lives in the option descriptions.
//
// The SDK reads TYPESAFE_API_KEY itself; this package never sees the key.
package jev

import (
	"context"
	"fmt"

	typesafe "github.com/nibir1/typesafe-go"

	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// QuestionID is the id of the one question asked.
const QuestionID = "better_plan"

// Judge implements ports.Judge on Jev.
type Judge struct {
	API   typesafe.API
	Model string // empty: the SDK default (jev-latest)
}

var _ ports.Judge = Judge{}

// New builds a Judge on a real client. The key comes from TYPESAFE_API_KEY.
func New(model string) (Judge, error) {
	c, err := typesafe.NewClient()
	if err != nil {
		return Judge{}, err
	}
	return Judge{API: c, Model: model}, nil
}

// Name implements ports.Judge.
func (j Judge) Name() string {
	if j.Model == "" {
		return "jev"
	}
	return "jev/" + j.Model
}

// question is the single Choice asked of every pair.
var question = typesafe.Choice{
	Instructions: "Which plan better accomplishes the user's request, given how the original attempt went?",
	Criteria: typesafe.Options{
		"a":    "Plan A is clearly better: it avoids the original failure with no unnecessary or unrequested steps.",
		"b":    "Plan B is clearly better: it avoids the original failure with no unnecessary or unrequested steps.",
		"same": "Neither plan is clearly better than the other.",
	},
}

// Compare implements ports.Judge.
func (j Judge) Compare(ctx context.Context, original types.Episode, outcomes []types.Outcome, with, without types.ReplayResult) (types.Verdict, error) {
	return j.ComparePair(ctx, judge.NewPair(original, outcomes, with, without))
}

// ComparePair judges a pair as already presented.
func (j Judge) ComparePair(ctx context.Context, pair judge.Pair) (types.Verdict, error) {
	resp, err := j.API.SystemOne(ctx, &typesafe.SystemOneRequest{
		State:     pair,
		Model:     j.Model,
		Questions: map[string]typesafe.Question{QuestionID: question},
	})
	if err != nil {
		return types.Verdict{}, fmt.Errorf("jev: %w", err)
	}
	ans, err := resp.Choice(QuestionID)
	if err != nil {
		return types.Verdict{}, fmt.Errorf("jev: %w", err)
	}
	p, _ := ans.ProbabilityOf(ans.Choice)
	return pair.Verdict(ans.Choice, p, fmt.Sprintf("jev chose %q with p=%.2f (confidence %.2f)", ans.Choice, p, ans.Confidence)), nil
}
