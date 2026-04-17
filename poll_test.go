package telebot

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoll(t *testing.T) {
	assert.True(t, (&Poll{Type: PollRegular}).IsRegular())
	assert.True(t, (&Poll{Type: PollQuiz}).IsQuiz())

	p := &Poll{}
	opts := []PollOption{{Text: "Option 1"}, {Text: "Option 2"}}
	p.AddOptions(opts[0].Text, opts[1].Text)
	assert.Equal(t, opts, p.Options)
}

func TestPollSend(t *testing.T) {
	if b == nil {
		t.Skip("Cached bot instance is bad (probably wrong or empty TELEBOT_SECRET)")
	}
	if userID == 0 {
		t.Skip("USER_ID is required for Poll methods test")
	}

	_, err := b.Send(user, &Poll{}) // empty poll
	assert.Equal(t, ErrBadPollOptions, err)

	poll := &Poll{
		Type:          PollQuiz,
		Question:      "Test Poll",
		CloseUnixdate: time.Now().Unix() + 60,
		Explanation:   "Explanation",
	}
	poll.AddOptions("1", "2")

	msg, err := b.Send(user, poll)
	require.NoError(t, err)
	assert.Equal(t, poll.Type, msg.Poll.Type)
	assert.Equal(t, poll.Question, msg.Poll.Question)
	assert.Equal(t, poll.Options, msg.Poll.Options)
	assert.Equal(t, poll.CloseUnixdate, msg.Poll.CloseUnixdate)
	assert.Equal(t, poll.CloseDate(), msg.Poll.CloseDate())

	p, err := b.StopPoll(msg)
	require.NoError(t, err)
	assert.Equal(t, poll.Options, p.Options)
	assert.Equal(t, 0, p.VoterCount)
}

func TestPollSendParamsUsesCurrentBotAPIFields(t *testing.T) {
	poll := &Poll{
		Type:                   PollQuiz,
		Question:               "What is the answer?",
		QuestionEntities:       []MessageEntity{{Type: EntityCustomEmoji, Offset: 0, Length: 2, CustomEmojiID: "question-emoji"}},
		Options:                []PollOption{{Text: "One", ParseMode: ModeMarkdown}, {Text: "Two", Entities: []MessageEntity{{Type: EntityCustomEmoji, Offset: 0, Length: 2, CustomEmojiID: "option-emoji"}}}},
		Anonymous:              true,
		AllowsRevoting:         true,
		ShuffleOptions:         true,
		AllowAddingOptions:     true,
		HideResultsUntilCloses: true,
		CorrectOptionIDs:       []int{0, 1},
		Explanation:            "Explanation",
		Entities:               []MessageEntity{{Type: EntityCustomEmoji, Offset: 0, Length: 2, CustomEmojiID: "explanation-emoji"}},
		Description:            "Description",
		DescriptionEntities:    []MessageEntity{{Type: EntityBold, Offset: 0, Length: 11}},
		OpenPeriod:             30,
	}

	params, err := poll.sendParams(&Chat{ID: 42})
	require.NoError(t, err)

	assert.Equal(t, "42", params["chat_id"])
	assert.Equal(t, "What is the answer?", params["question"])
	assert.NotContains(t, params, "question_parse_mode")
	assert.NotContains(t, params, "explanation_parse_mode")
	assert.NotContains(t, params, "description_parse_mode")
	assert.Equal(t, "true", params["allows_revoting"])
	assert.Equal(t, "true", params["shuffle_options"])
	assert.Equal(t, "true", params["allow_adding_options"])
	assert.Equal(t, "true", params["hide_results_until_closes"])
	assert.Equal(t, "30", params["open_period"])
	assert.JSONEq(t, `[0,1]`, params["correct_option_ids"])
	assert.JSONEq(t, `[{"type":"custom_emoji","offset":0,"length":2,"custom_emoji_id":"question-emoji"}]`, params["question_entities"])
	assert.JSONEq(t, `[{"type":"custom_emoji","offset":0,"length":2,"custom_emoji_id":"explanation-emoji"}]`, params["explanation_entities"])
	assert.JSONEq(t, `[{"type":"bold","offset":0,"length":11}]`, params["description_entities"])
	assert.JSONEq(t, `[{"text":"One","text_parse_mode":"Markdown"},{"text":"Two","text_entities":[{"type":"custom_emoji","offset":0,"length":2,"custom_emoji_id":"option-emoji"}]}]`, params["options"])
}

func TestPollUnmarshalSupportsCurrentAndLegacyCorrectOptions(t *testing.T) {
	var current Poll
	err := json.Unmarshal([]byte(`{"correct_option_ids":[0,2]}`), &current)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 2}, current.CorrectOptionIDs)
	assert.Equal(t, 0, current.CorrectOption)

	var legacy Poll
	err = json.Unmarshal([]byte(`{"correct_option_id":1}`), &legacy)
	require.NoError(t, err)
	assert.Equal(t, []int{1}, legacy.CorrectOptionIDs)
	assert.Equal(t, 1, legacy.CorrectOption)
}

func TestPollSendParamsKeepsLegacyCorrectOptionCompatibility(t *testing.T) {
	poll := &Poll{
		Type:          PollQuiz,
		Question:      "Legacy quiz",
		Options:       []PollOption{{Text: "A"}, {Text: "B"}},
		CorrectOption: 0,
	}

	params, err := poll.sendParams(&Chat{ID: 7})
	require.NoError(t, err)
	assert.JSONEq(t, `[0]`, params["correct_option_ids"])
}
