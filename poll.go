package telebot

import (
	"encoding/json"
	"time"
)

// PollType defines poll types.
type PollType string

const (
	// NOTE:
	// Despite "any" type isn't described in documentation,
	// it needed for proper KeyboardButtonPollType marshaling.
	PollAny PollType = "any"

	PollQuiz    PollType = "quiz"
	PollRegular PollType = "regular"
)

// Poll contains information about a poll.
type Poll struct {
	ID         string       `json:"id"`
	Type       PollType     `json:"type"`
	Question   string       `json:"question"`
	Options    []PollOption `json:"options"`
	VoterCount int          `json:"total_voter_count"`

	// (Optional)
	Closed                 bool            `json:"is_closed,omitempty"`
	MultipleAnswers        bool            `json:"allows_multiple_answers,omitempty"`
	AllowsRevoting         bool            `json:"allows_revoting,omitempty"`
	ShuffleOptions         bool            `json:"shuffle_options,omitempty"`
	AllowAddingOptions     bool            `json:"allow_adding_options,omitempty"`
	HideResultsUntilCloses bool            `json:"hide_results_until_closes,omitempty"`
	CorrectOptionIDs       []int           `json:"correct_option_ids,omitempty"`
	Explanation            string          `json:"explanation,omitempty"`
	ParseMode              ParseMode       `json:"explanation_parse_mode,omitempty"`
	Entities               []MessageEntity `json:"explanation_entities,omitempty"`
	QuestionParseMode      ParseMode       `json:"question_parse_mode,omitempty"`
	QuestionEntities       []MessageEntity `json:"question_entities,omitempty"`
	Description            string          `json:"description,omitempty"`
	DescriptionParseMode   ParseMode       `json:"description_parse_mode,omitempty"`
	DescriptionEntities    []MessageEntity `json:"description_entities,omitempty"`
	// Deprecated: use CorrectOptionIDs for send operations and multi-answer quizzes.
	// This alias is kept for compatibility with older code paths.
	CorrectOption int `json:"-"`

	// True by default, shouldn't be omitted.
	Anonymous bool `json:"is_anonymous"`

	// (Mutually exclusive)
	OpenPeriod    int   `json:"open_period,omitempty"`
	CloseUnixdate int64 `json:"close_date,omitempty"`
}

// UnmarshalJSON keeps Poll aligned with the current Bot API while preserving
// the legacy CorrectOption field as a compatibility alias for single-answer quizzes.
func (p *Poll) UnmarshalJSON(data []byte) error {
	type pollAlias Poll

	var aux struct {
		pollAlias
		LegacyCorrectOption *int `json:"correct_option_id,omitempty"`
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*p = Poll(aux.pollAlias)

	switch {
	case len(p.CorrectOptionIDs) > 0:
		p.CorrectOption = p.CorrectOptionIDs[0]
	case aux.LegacyCorrectOption != nil:
		p.CorrectOption = *aux.LegacyCorrectOption
		p.CorrectOptionIDs = []int{*aux.LegacyCorrectOption}
	}

	return nil
}

// PollOption contains information about one answer option in a poll.
type PollOption struct {
	Text       string          `json:"text"`
	VoterCount int             `json:"voter_count"`
	ParseMode  ParseMode       `json:"text_parse_mode,omitempty"`
	Entities   []MessageEntity `json:"text_entities,omitempty"`
}

// InputPollOption mirrors the Bot API input-only object used by sendPoll.
// PollOption remains the ergonomic in-memory type for received polls.
type InputPollOption struct {
	Text      string          `json:"text"`
	ParseMode ParseMode       `json:"text_parse_mode,omitempty"`
	Entities  []MessageEntity `json:"text_entities,omitempty"`
}

// PollAnswer represents an answer of a user in a non-anonymous poll.
type PollAnswer struct {
	PollID  string `json:"poll_id"`
	Sender  *User  `json:"user"`
	Chat    *Chat  `json:"voter_chat"`
	Options []int  `json:"option_ids"`
}

// IsRegular says whether poll is a regular.
func (p *Poll) IsRegular() bool {
	return p.Type == PollRegular
}

// IsQuiz says whether poll is a quiz.
func (p *Poll) IsQuiz() bool {
	return p.Type == PollQuiz
}

// CloseDate returns the close date of poll in local time.
func (p *Poll) CloseDate() time.Time {
	return time.Unix(p.CloseUnixdate, 0)
}

// AddOptions adds text options to the poll.
func (p *Poll) AddOptions(opts ...string) {
	for _, t := range opts {
		p.Options = append(p.Options, PollOption{Text: t})
	}
}
