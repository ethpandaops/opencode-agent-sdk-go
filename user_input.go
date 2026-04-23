package opencodesdk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/mcp/bridge"
)

// AskUserQuestionToolName is the MCP tool name the SDK registers when
// WithOnUserInput is set. Mirrors the tool Claude Code exposes so
// prompts and host translators that target Claude Code transfer
// unchanged.
const AskUserQuestionToolName = "AskUserQuestion"

// QuestionOption represents a selectable choice in a user-input
// question.
type QuestionOption struct {
	Label       string
	Description string
}

// Question is a single prompt the agent wants the user to answer.
type Question struct {
	ID          string
	Header      string
	Question    string
	MultiSelect bool
	IsOther     bool
	IsSecret    bool
	Options     []QuestionOption
}

// UserInputRequest is the payload delivered to a UserInputCallback when
// the agent invokes AskUserQuestion.
type UserInputRequest struct {
	Questions []Question
}

// UserInputAnswer is the response for a single question keyed by
// Question.ID.
type UserInputAnswer struct {
	Answers []string
}

// UserInputResponse is returned by a UserInputCallback. Answers is
// keyed by Question.ID; entries whose ID does not match a question in
// the original request are ignored.
type UserInputResponse struct {
	Answers map[string]*UserInputAnswer
}

// UserInputCallback handles AskUserQuestion invocations. Implementations
// receive the parsed questions and return the user's answers keyed by
// Question.ID.
//
// Sister SDK parity: mirrors claude-agent-sdk-go's
// user_input.Callback. Hosts that already translate UserInputRequest
// for Claude Code reuse the same handler verbatim.
type UserInputCallback func(ctx context.Context, req *UserInputRequest) (*UserInputResponse, error)

// WithOnUserInput registers a callback that handles AskUserQuestion
// invocations from the agent. When set, the SDK registers an implicit
// in-process MCP tool named AskUserQuestion via the loopback bridge
// (the same path WithSDKTools uses) so the agent can ask the user
// structured, multi-option clarifying questions and receive typed
// answers.
//
// Example:
//
//	cb := func(ctx context.Context, req *opencodesdk.UserInputRequest) (*opencodesdk.UserInputResponse, error) {
//	    answers := map[string]*opencodesdk.UserInputAnswer{}
//	    for _, q := range req.Questions {
//	        if len(q.Options) > 0 {
//	            answers[q.ID] = &opencodesdk.UserInputAnswer{Answers: []string{q.Options[0].Label}}
//	        }
//	    }
//	    return &opencodesdk.UserInputResponse{Answers: answers}, nil
//	}
//
//	c, _ := opencodesdk.NewClient(opencodesdk.WithOnUserInput(cb))
//
// The tool cannot coexist with a user-supplied WithSDKTools entry of
// the same name; Client.Start returns an error when both are
// registered.
func WithOnUserInput(cb UserInputCallback) Option {
	return func(o *options) { o.onUserInput = cb }
}

// askUserQuestionSchema is the JSON schema the SDK advertises for the
// implicit AskUserQuestion tool. Matches the shape Claude Code's
// built-in AskUserQuestion uses so models trained on it call our tool
// with no adaptation.
func askUserQuestionSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type":        "array",
				"minItems":    1,
				"description": "Ordered list of questions to present to the user.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Stable identifier for correlating answers; optional (SDK assigns one if absent).",
						},
						"header": map[string]any{
							"type":        "string",
							"description": "Short, title-cased summary rendered above the question.",
						},
						"question": map[string]any{
							"type":        "string",
							"description": "The full question text presented to the user.",
						},
						"multiSelect": map[string]any{
							"type":        "boolean",
							"description": "When true, the user may pick more than one option.",
						},
						"isOther": map[string]any{
							"type":        "boolean",
							"description": "When true, the user may type a free-form response in place of choosing an option.",
						},
						"isSecret": map[string]any{
							"type":        "boolean",
							"description": "When true, the user's response is treated as sensitive and hidden in UIs.",
						},
						"options": map[string]any{
							"type":        "array",
							"description": "Selectable options. Omit for free-form questions.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label":       map[string]any{"type": "string"},
									"description": map[string]any{"type": "string"},
								},
								"required": []string{"label"},
							},
						},
					},
					"required": []string{"question"},
				},
			},
		},
		"required": []string{"questions"},
	}
}

// askUserQuestionDescription mirrors Claude Code's wording so
// instruction transfer across SDKs is lossless.
const askUserQuestionDescription = "Ask the user structured clarifying questions when their request is ambiguous. " +
	"Provide up to a few multiple-choice questions the user can answer quickly. " +
	"Each question may include an 'options' list; omit it for free-form prompts. " +
	"Prefer this tool over sending a plain-text numbered list in the assistant message."

// askUserQuestionBridgeDef builds the bridge.ToolDef the client
// appends to toolsToBridgeDefs when WithOnUserInput is set.
func askUserQuestionBridgeDef(cb UserInputCallback, logger *slog.Logger) bridge.ToolDef {
	return bridge.ToolDef{
		Name:        AskUserQuestionToolName,
		Description: askUserQuestionDescription,
		Schema:      askUserQuestionSchema(),
		Handler: func(ctx context.Context, in map[string]any) (*bridge.ToolOutput, error) {
			req, err := parseUserInputRequest(in)
			if err != nil {
				if logger != nil {
					logger.DebugContext(ctx, "AskUserQuestion input parse error", slog.Any("error", err))
				}

				return nil, fmt.Errorf("opencodesdk: parsing AskUserQuestion input: %w", err)
			}

			resp, err := cb(ctx, req)
			if err != nil {
				return nil, fmt.Errorf("opencodesdk: WithOnUserInput callback: %w", err)
			}

			payload, err := buildUserInputResultPayload(req, resp)
			if err != nil {
				return nil, fmt.Errorf("opencodesdk: encoding AskUserQuestion response: %w", err)
			}

			text, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("opencodesdk: marshaling AskUserQuestion response: %w", err)
			}

			return &bridge.ToolOutput{
				Text:       string(text),
				Structured: payload,
			}, nil
		},
	}
}

// parseUserInputRequest decodes the AskUserQuestion tool input into
// the typed UserInputRequest. Accepts both camelCase and snake_case
// field spellings so prompts authored for Claude Code, Codex, or the
// SDK's own schema all parse.
func parseUserInputRequest(input map[string]any) (*UserInputRequest, error) {
	if input == nil {
		return nil, fmt.Errorf("missing tool input")
	}

	rawQuestions, ok := input["questions"].([]any)
	if !ok || len(rawQuestions) == 0 {
		return nil, fmt.Errorf("missing questions")
	}

	req := &UserInputRequest{
		Questions: make([]Question, 0, len(rawQuestions)),
	}

	for i, raw := range rawQuestions {
		qMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		q := Question{}

		q.ID, _ = qMap["id"].(string)
		if q.ID == "" {
			q.ID = fmt.Sprintf("question_%d", i+1)
		}

		q.Question, _ = qMap["question"].(string)
		q.Header, _ = qMap["header"].(string)

		q.MultiSelect, _ = qMap["multiSelect"].(bool)
		if !q.MultiSelect {
			if v, ok := qMap["multi_select"].(bool); ok {
				q.MultiSelect = v
			}
		}

		if v, ok := qMap["isOther"].(bool); ok {
			q.IsOther = v
		}

		if v, ok := qMap["isSecret"].(bool); ok {
			q.IsSecret = v
		}

		if rawOpts, ok := qMap["options"].([]any); ok {
			q.Options = make([]QuestionOption, 0, len(rawOpts))

			for _, rawOpt := range rawOpts {
				optMap, ok := rawOpt.(map[string]any)
				if !ok {
					continue
				}

				opt := QuestionOption{}
				opt.Label, _ = optMap["label"].(string)
				opt.Description, _ = optMap["description"].(string)

				q.Options = append(q.Options, opt)
			}
		}

		req.Questions = append(req.Questions, q)
	}

	if len(req.Questions) == 0 {
		return nil, fmt.Errorf("no parseable questions")
	}

	return req, nil
}

// buildUserInputResultPayload maps the callback's UserInputResponse
// onto a JSON-friendly shape keyed by question ID. Only answers whose
// ID matches one of the original questions are retained.
func buildUserInputResultPayload(req *UserInputRequest, resp *UserInputResponse) (map[string]any, error) {
	if resp == nil || len(resp.Answers) == 0 {
		return nil, fmt.Errorf("answers cannot be empty")
	}

	out := make(map[string][]string, len(req.Questions))

	for _, q := range req.Questions {
		answer, ok := resp.Answers[q.ID]
		if !ok || answer == nil || len(answer.Answers) == 0 {
			continue
		}

		values := make([]string, 0, len(answer.Answers))

		for _, v := range answer.Answers {
			if v != "" {
				values = append(values, v)
			}
		}

		if len(values) > 0 {
			out[q.ID] = values
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no valid answers matched question IDs")
	}

	answersAny := make(map[string]any, len(out))
	for k, v := range out {
		answersAny[k] = v
	}

	return map[string]any{"answers": answersAny}, nil
}
