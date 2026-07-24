package telebot

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	e "github.com/ChatDetectiveORG/shared/errors"
	"github.com/ChatDetectiveORG/shared/utils"
	"github.com/go-pg/pg/v10/orm"
	"github.com/gomodule/redigo/redis"
)

// b.Write([]t.T{t.T("Обычный текст"), t.B("Жирный текст"), t.B(t.I("Жирный подчёркнутый")), t.E("#", "1111"), t.T("Каждый элемент - новая строка. А выше кастомное эмодзи")})

// Message builder is a helper to build comlex formatted messages
// It allows to build mdv2 messages and messages formatted via hardcoded entities
//
// ToDo: Add advanced keyboard building
type MessageBuilder struct {
	Mdv2Enabled bool

	text     string
	entities []MessageEntity

	keyboard   [][]InlineButton
	currentRow []InlineButton

	builder        *strings.Builder
	cursorPosition int

	messageID   int
	files       []mediaAttachment
	mirrorFiles []MirrorFileAsset
}
func (self *MessageBuilder) AddMirrorFile(asset MirrorFileAsset) *MessageBuilder {
	self.mirrorFiles = append(self.mirrorFiles, asset)
	return self
}

func (self *MessageBuilder) ConsumeMirrorFiles() []MirrorFileAsset {
	assets := self.mirrorFiles
	self.mirrorFiles = nil
	return assets
}


type mediaKind int

const (
	mediaKindPhoto mediaKind = iota
	mediaKindVideo
	mediaKindAnimation
	mediaKindAudio
	mediaKindVoice
	mediaKindDocument
)

type mediaAttachment struct {
	file     File
	mimeType string
	fileName string
	kind     mediaKind
}

func (self *MessageBuilder) checkBuilder() {
	if self.builder == nil {
		self.builder = &strings.Builder{}
	}
}

// Deprecated: Use WriteSlice instead
func (self *MessageBuilder) writeString(s string, escape bool) {
	self.checkBuilder()

	if self.Mdv2Enabled && escape {
		s = utils.EscapeMarkdownV2(s)
	}

	self.builder.WriteString(s)
	self.cursorPosition += utils.TgLen(s)
}

// Deprecated: Use WriteSlice instead
func (self *MessageBuilder) WriteString(s string, specialFormatting ...TextFormat) *MessageBuilder {
	self.checkBuilder()

	if len(specialFormatting) == 0 {
		self.writeString(s, true)

		return self
	}

	if self.Mdv2Enabled {
		if len(specialFormatting) > 1 {
			self.writeString(specialFormatting[0].toMdV2Tag(s, specialFormatting[1:]...), false)
		} else {
			self.writeString(specialFormatting[0].toMdV2Tag(s), false)
		}

		return self
	}

	offset := self.cursorPosition
	self.writeString(s, true)

	for _, format := range specialFormatting {
		entity := format.toTelebotTag(s, offset)
		if entity.Type != "" {
			self.entities = append(self.entities, entity)
		}
	}

	return self
}

// Deprecated: Use WriteSlice instead
func (self *MessageBuilder) WriteNextLine(s string, specialFormatting ...TextFormat) *MessageBuilder {
	self.checkBuilder()

	self.WriteString("\n"+s, specialFormatting...)

	return self
}

func (self *MessageBuilder) CustomeEmoji(e string, id string) *MessageBuilder {
	self.checkBuilder()

	self.WriteString(e, TextFormat{Type: FormatLink}.WithCustomEmojiID(id))

	return self
}

func (self *MessageBuilder) AddButton(button InlineButton) *MessageBuilder {
	self.currentRow = append(self.currentRow, button)
	return self
}

func (self *MessageBuilder) NextRow() *MessageBuilder {
	self.keyboard = append(self.keyboard, self.currentRow)
	self.currentRow = []InlineButton{}
	return self
}

type CreateGenericKeyboardParams struct {
	ChatID     int64
	PageUnique string

	ButtonsPerPage   int
	ButtonsPerRow    int
	ArrowForwardText string
	ArrowBackText    string
	ShowNavigation   bool
	MergeButtons     [][]InlineButton

	ButtonConversionArgs TelegramButtonConversionArgs
}

func (self *CreateGenericKeyboardParams) fillDefaults() *CreateGenericKeyboardParams {
	if self.ButtonsPerPage == 0 {
		self.ButtonsPerPage = defaultCreateGenericKeyboardParams.ButtonsPerPage
	}
	if self.ButtonsPerRow == 0 {
		self.ButtonsPerRow = defaultCreateGenericKeyboardParams.ButtonsPerRow
	}
	if self.ArrowForwardText == "" {
		self.ArrowForwardText = defaultCreateGenericKeyboardParams.ArrowForwardText
	}
	if self.ArrowBackText == "" {
		self.ArrowBackText = defaultCreateGenericKeyboardParams.ArrowBackText
	}
	if !self.ShowNavigation {
		self.ShowNavigation = defaultCreateGenericKeyboardParams.ShowNavigation
	}

	return self
}

var defaultCreateGenericKeyboardParams = CreateGenericKeyboardParams{
	ButtonsPerPage:   8,
	ButtonsPerRow:    2,
	ArrowForwardText: ">->>",
	ArrowBackText:    "<<-<",
	ShowNavigation:   true,
}

// Returns: page, error
func (self *MessageBuilder) updateRedis(redisConn redis.Conn, chatID int64, pageUnique string, pageDelta int) (int, *e.ErrorInfo) {
	key := fmt.Sprintf("keyboard:%s:%d", pageUnique, chatID)
	p, eRaw := redis.Int(redisConn.Do("HGET", key, "page"))
	if eRaw != nil {
		p = 0
	}

	p += pageDelta

	if _, eRaw = redisConn.Do("HSET", key, "page", p); eRaw != nil {
		return p, e.FromError(eRaw, "failed to update redis").WithSeverity(e.Notice)
	}
	if _, eRaw = redisConn.Do("EXPIRE", key, 600); eRaw != nil {
		return p, e.FromError(eRaw, "failed to update redis").WithSeverity(e.Notice)
	}

	return p, e.Nil()
}

type TelegramButtonConversionArgs struct {
	pageUnique         string
	AdditionalData     map[string]any
	CallbackDataProducer func(string) string
}

func (self *TelegramButtonConversionArgs) setPageUnique(pageUnique string) {
	self.pageUnique = pageUnique
}

func (self *TelegramButtonConversionArgs) PageUnique() string {
	return self.pageUnique
}

type Buttonable interface {
	ToTelegramButton(db orm.DB, args TelegramButtonConversionArgs) InlineButton
}

// parseKeyboardPageDelta reads pageDelta from callback data (action?...&pageDelta=N).
// Empty callbackData means the first open (delta 0).
func parseKeyboardPageDelta(callbackData string) (int, bool) {
	if callbackData == "" {
		return 0, true
	}

	callbackDataParams := utils.ParseCallbackData(callbackData)
	deltaStr, ok := callbackDataParams["pageDelta"]
	if !ok {
		deltaStr = "0"
	}

	delta, err := strconv.Atoi(deltaStr)
	if e.IsNonNil(err) {
		return 0, false
	}

	return delta, true
}

func CreateGenericKeyboard[T Buttonable](
	builder *MessageBuilder,
	query *orm.Query,
	redisConn redis.Conn,
	postgresDb orm.DB,
	callbackData string,
	params CreateGenericKeyboardParams,
) {
	if params.ChatID == 0 || params.PageUnique == "" {
		return
	}

	delta, ok := parseKeyboardPageDelta(callbackData)
	if !ok {
		return
	}

	page, err := builder.updateRedis(redisConn, params.ChatID, params.PageUnique, delta)
	if e.IsNonNil(err) {
		return
	}

	params.fillDefaults()

	count, eRaw := query.Count()
	if eRaw != nil {
		return
	}

	if count <= 0 {
		return
	}

	maxPage := int(math.Ceil(float64(count)/float64(params.ButtonsPerPage))) - 1

	if page > maxPage {
		page, err = builder.updateRedis(redisConn, params.ChatID, params.PageUnique, page*-1)
		if e.IsNonNil(err) {
			return
		}
	}
	if page < 0 {
		page, err = builder.updateRedis(redisConn, params.ChatID, params.PageUnique, maxPage+1)
		if e.IsNonNil(err) {
			return
		}
	}

	var buttons []T

	eRaw = query.Model(&buttons).Limit(params.ButtonsPerPage).Offset(page * params.ButtonsPerPage).Select()
	if e.IsNonNil(eRaw) {
		return
	}

	params.ButtonConversionArgs.setPageUnique(params.PageUnique)

	for _, b := range buttons {
		buttonable, ok := any(b).(Buttonable)
		if !ok {
			continue
		}

		button := buttonable.ToTelegramButton(postgresDb, params.ButtonConversionArgs)
		builder.AddButton(button)

		if len(builder.currentRow) >= params.ButtonsPerRow {
			builder.NextRow()
		}
	}

	if len(builder.currentRow) > 0 {
		builder.NextRow()
	}

	if len(params.MergeButtons) != 0 {
		for _, row := range params.MergeButtons {
			for _, button := range row {
				builder.AddButton(button)
			}

			builder.NextRow()
		}
	}

	if maxPage > 0 && params.ShowNavigation {
		builder.AddButton(InlineButton{Text: params.ArrowBackText, Data: utils.DumpCallbackData(params.PageUnique, map[string]any{"pageDelta": -1})})
		builder.AddButton(InlineButton{Text: params.ArrowForwardText, Data: utils.DumpCallbackData(params.PageUnique, map[string]any{"pageDelta": 1})})
		builder.NextRow()
	}
}

func (self *MessageBuilder) WithMessageID(messageID int) *MessageBuilder {
	self.messageID = messageID
	return self
}

// MediaGroup is a serializable Telegram album payload built from MessageBuilder.
type MediaGroup struct {
	Chat     *Chat
	Messages []*Message
	Silent   bool
}

// BuildMediaGroup assembles an album from all files added via AddFile.
// Caption and entities from WriteString are applied to the first media item.
func (self *MessageBuilder) BuildMediaGroup(chatID int64) (*MediaGroup, bool) {
	if len(self.files) == 0 {
		return nil, false
	}

	if len(self.currentRow) > 0 {
		self.NextRow()
	}

	caption := ""
	if self.builder != nil {
		caption = self.builder.String()
	}

	messages := make([]*Message, 0, len(self.files))
	for _, attachment := range self.files {
		mediaMessage := &Message{}
		applyMediaAttachment(mediaMessage, attachment)
		messages = append(messages, mediaMessage)
	}

	if caption != "" {
		setMediaGroupCaption(messages[0], caption)
		if !self.Mdv2Enabled && len(self.entities) > 0 {
			messages[0].CaptionEntities = self.entities
		}
	}

	if len(self.keyboard) > 0 {
		applySendOptions(messages[0], &SendOptions{
			ReplyMarkup: &ReplyMarkup{InlineKeyboard: self.keyboard},
		})
	}

	return &MediaGroup{
		Chat:     &Chat{ID: chatID},
		Messages: messages,
	}, true
}

func (self *MessageBuilder) Build(chatID int64) *Message {
	if len(self.currentRow) > 0 {
		self.NextRow()
	}

	text := ""
	if self.builder != nil {
		text = self.builder.String()
	}

	msg := &Message{
		Chat: &Chat{ID: chatID},
		Text: text,
		ReplyMarkup: &ReplyMarkup{
			InlineKeyboard: self.keyboard,
		},
	}

	if self.messageID != 0 {
		msg.ID = self.messageID
	}

	if !self.Mdv2Enabled {
		msg.Entities = self.entities
	}

	if len(self.files) == 1 {
		applyMediaAttachment(msg, self.files[0])
		self.applyCaption(msg, text)
	}

	return msg
}

func setMediaGroupCaption(msg *Message, caption string) {
	if msg == nil {
		return
	}

	msg.Caption = caption

	switch {
	case msg.Photo != nil:
		msg.Photo.Caption = caption
	case msg.Video != nil:
		msg.Video.Caption = caption
	case msg.Document != nil:
		msg.Document.Caption = caption
	case msg.Audio != nil:
		msg.Audio.Caption = caption
	case msg.Animation != nil:
		msg.Animation.Caption = caption
	}
}

func applySendOptions(msg *Message, sendOptions *SendOptions) {
	if msg == nil || sendOptions == nil {
		return
	}

	if msg.ReplyTo == nil {
		msg.ReplyTo = sendOptions.ReplyTo
	}

	msg.ReplyMarkup = sendOptions.ReplyMarkup
	if sendOptions.DisableWebPagePreview {
		if msg.PreviewOptions == nil {
			msg.PreviewOptions = &PreviewOptions{}
		}
		msg.PreviewOptions.Disabled = true
	}

	for _, e := range sendOptions.Entities {
		msg.Entities = append(msg.Entities, e)
	}
	msg.Protected = sendOptions.Protected
	msg.HasMediaSpoiler = sendOptions.HasSpoiler
	msg.EffectID = sendOptions.EffectID
}

type Format string

const (
	FormatBold          Format = "bold"
	FormatItalic        Format = "italic"
	FormatUnderline     Format = "underline"
	FormatLink          Format = "link"
	FormatBlockquote    Format = "blockquote"
	FormatMono          Format = "mono"
	FormatSpoiler       Format = "spoiler"
	FormatStrikethrough Format = "strikethrough"
)

type TextFormat struct {
	Type Format
	URL  string

	isCustomEmoji bool
}

func (self TextFormat) WithCustomEmojiID(id string) TextFormat {
	self.URL = "tg://emoji?id=" + id
	self.isCustomEmoji = true
	return self
}

func (self TextFormat) WithUserMention(id int64) TextFormat {
	self.URL = "tg://user?id=" + strconv.FormatInt(id, 10)
	return self
}

func (self *TextFormat) tagWrap() string {
	switch self.Type {
	case FormatBold:
		return "*" + "%s" + "*"
	case FormatItalic:
		return "_" + "%s" + "_"
	case FormatUnderline:
		return "__" + "%s" + "__"
	case FormatStrikethrough:
		return "~" + "%s" + "~"
	case FormatLink:
		res := "[" + "%s" + "](" + self.URL + ")"
		if self.isCustomEmoji {
			res = "!" + res
		}
		return res
	case FormatBlockquote:
		return "\n>%s"
	case FormatMono:
		return "`" + "%s" + "`"
	case FormatSpoiler:
		return "||" + "%s" + "||"
	case "mention":
		return "%s"
	default:
		return "%s"
	}
}

func (self *TextFormat) toMdV2Tag(content string, other ...TextFormat) string {
	content = utils.EscapeMarkdownV2(content)

	typeMap := make(map[Format]TextFormat)
	order := []Format{}
	for _, f := range other {
		typeMap[f.Type] = f
		order = append(order, f.Type)
	}
	typeMap[self.Type] = *self
	order = append(order, self.Type)

	uniqueTypes := []TextFormat{}
	seen := map[Format]struct{}{}
	for i := len(order) - 1; i >= 0; i-- {
		typ := order[i]
		if _, ok := seen[typ]; !ok {
			uniqueTypes = append([]TextFormat{typeMap[typ]}, uniqueTypes...)
			seen[typ] = struct{}{}
		}
	}

	formatPriority := map[Format]int{
		FormatMono:          7,
		FormatBlockquote:    6,
		FormatBold:          5,
		FormatItalic:        4,
		FormatUnderline:     3,
		FormatSpoiler:       2,
		FormatStrikethrough: 1,
		FormatLink:          0,
	}
	slices.SortFunc(uniqueTypes, func(a, b TextFormat) int {
		ap, aok := formatPriority[a.Type]
		bp, bok := formatPriority[b.Type]
		if !aok {
			ap = 100
		}
		if !bok {
			bp = 100
		}
		return ap - bp
	})

	for _, format := range uniqueTypes {
		if format.Type == FormatBlockquote {
			content = strings.ReplaceAll(content, "\n", "\n>")
		}
		content = fmt.Sprintf(format.tagWrap(), content)
	}

	return content
}

func (self *TextFormat) toTelebotTag(content string, offset int) MessageEntity {
	contentLen := utils.TgLen(content)

	switch self.Type {
	case FormatBold:
		return MessageEntity{
			Type:   EntityBold,
			Offset: offset,
			Length: contentLen,
		}
	case FormatItalic:
		return MessageEntity{
			Type:   EntityItalic,
			Offset: offset,
			Length: contentLen,
		}
	case FormatUnderline:
		return MessageEntity{
			Type:   EntityUnderline,
			Offset: offset,
			Length: contentLen,
		}
	case FormatLink:
		return MessageEntity{
			Type:   EntityTextLink,
			Offset: offset,
			Length: contentLen,
			URL:    self.URL,
		}
	case FormatStrikethrough:
		return MessageEntity{
			Type:   EntityStrikethrough,
			Offset: offset,
			Length: contentLen,
		}
	case FormatBlockquote:
		return MessageEntity{
			Type:   EntityBlockquote,
			Offset: offset,
			Length: contentLen,
		}
	case FormatMono:
		return MessageEntity{
			Type:   EntityCode,
			Offset: offset,
			Length: contentLen,
		}
	case FormatSpoiler:
		return MessageEntity{
			Type:   EntitySpoiler,
			Offset: offset,
			Length: contentLen,
		}
	default:
		return MessageEntity{Type: ""}
	}

}

func (self *MessageBuilder) Write(contents ...text) *MessageBuilder {
	self.checkBuilder()

	for _, t := range contents {
		if self.Mdv2Enabled {
			s := t.ToMdV2String()
			self.builder.WriteString(s)
			self.cursorPosition += utils.TgLen(s)

			continue
		}

		self.entities = append(self.entities, t.TelebotEntities(self.cursorPosition)...)

		s := t.Text()
		self.builder.WriteString(s)
		self.cursorPosition += utils.TgLen(s)
	}

	return self
}

type text interface {
	ToMdV2String() string
	TelebotEntities(offset int) []MessageEntity
	Text() string
}

type universalText struct {
	Contents string
	Upper    text

	MdV2tag       string
	TelebotEntity MessageEntity
}

func (self *universalText) ToMdV2String() string {
	if self.Upper != nil {
		return self.MdV2tag + self.Upper.ToMdV2String() + self.MdV2tag

	}

	return self.MdV2tag + utils.EscapeMarkdownV2(self.Contents) + self.MdV2tag
}

func (self *universalText) TelebotEntities(offset int) []MessageEntity {
	if self.TelebotEntity.Type == "" {
		return []MessageEntity{}
	}

	self.TelebotEntity.Length = utils.TgLen(self.Text())
	self.TelebotEntity.Offset = offset

	if self.Upper != nil {
		entities := self.Upper.TelebotEntities(offset)

		entities = append(entities, self.TelebotEntity)

		return entities
	}

	return []MessageEntity{self.TelebotEntity}
}

func (self *universalText) Text() string {
	if self.Upper != nil {
		return self.Upper.Text()
	}

	// fmt.Sprintf()

	return self.Contents
}

type Args struct {
	A         []any
	NoNewline bool
}

// Represents Plain Text
//
// Embeds fmt.Sprintf logic. Last arg reserved for newline control
func T(t string, a ...Args) text {
	postfix := "\n"

	if len(a) > 0 && a[0].NoNewline {
		postfix = ""
	}

	contents := t + postfix
	if len(a) > 0 && a[0].A != nil && len(a[0].A) > 0 {
		contents = fmt.Sprintf(strings.ReplaceAll(contents, "%", "%%"), a[0].A)
	}

	return &universalText{
		Contents:      contents,
		TelebotEntity: MessageEntity{Type: ""},
		MdV2tag:       "",
	}
}

// Represents Bold Text
func B(t text) text {
	return &universalText{
		MdV2tag: "*",
		TelebotEntity: MessageEntity{
			Type: EntityBold,
		},
		Upper: t,
	}
}

// Represents Italic Text
func I(t text) text {
	return &universalText{
		MdV2tag: "_",
		TelebotEntity: MessageEntity{
			Type: EntityItalic,
		},
		Upper: t,
	}
}

// Represents Underline Text
func U(t text) text {
	return &universalText{
		MdV2tag: "__",
		TelebotEntity: MessageEntity{
			Type: EntityUnderline,
		},
		Upper: t,
	}
}

func Strikethrough(t text) text {
	return &universalText{
		MdV2tag: "~",
		TelebotEntity: MessageEntity{
			Type: EntityStrikethrough,
		},
		Upper: t,
	}
}

func Mono(t text) text {
	return &universalText{
		MdV2tag: "`",
		TelebotEntity: MessageEntity{
			Type: EntityCode,
		},
		Upper: t,
	}
}

func Spoiler(t text) text {
	return &universalText{
		MdV2tag: "||",
		TelebotEntity: MessageEntity{
			Type: EntitySpoiler,
		},
		Upper: t,
	}
}

type link struct {
	Contents      string
	IsCustomEmoji bool
	URL           string
}

func (self *link) ToMdV2String() string {

	res := "[" + utils.EscapeMarkdownV2(self.Contents) + "](" + self.URL + ")"
	if self.IsCustomEmoji {
		res = "!" + res
	}

	return res
}

func (self *link) TelebotEntities(offset int) []MessageEntity {
	return []MessageEntity{{
		Type:   EntityTextLink,
		Offset: offset,
		Length: utils.TgLen(self.Contents),
		URL:    self.URL,
	}}
}

func (self *link) Text() string {
	return self.Contents
}

func Link(t string, url string) text {
	return &link{
		Contents: t,
		URL:      url,
	}
}

func UserMention(name string, ID int64) text {
	return &link{
		Contents: name,
		URL:      "tg://user?id=" + strconv.FormatInt(ID, 10),
	}
}

// Represents Custom Emoji
func E(emojiID string, placholder ...string) text {
	emoji := "👾"
	if len(placholder) > 0 {
		emoji = placholder[0]
	}

	return &link{
		Contents:      emoji,
		URL:           "tg://emoji?id=" + emojiID,
		IsCustomEmoji: true,
	}
}

func resolveBuilderFile(fileID, fallbackPath string) File {
	file := File{FileID: fileID}
	if fallbackPath != "" {
		file.FileLocal = fallbackPath
	}
	return file
}

func mimeTypeFromExtension(fileName string) string {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".mp4", ".mov":
		return "video/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".ogg", ".oga":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

func categorizeMediaFile(mimeType, fileName string) mediaKind {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if mimeType == "" {
		mimeType = mimeTypeFromExtension(fileName)
	}

	switch {
	case strings.HasPrefix(mimeType, "image/gif"):
		return mediaKindAnimation
	case strings.HasPrefix(mimeType, "image/"):
		return mediaKindPhoto
	case strings.HasPrefix(mimeType, "video/"):
		return mediaKindVideo
	case mimeType == "audio/ogg" || mimeType == "audio/opus" || strings.HasSuffix(strings.ToLower(fileName), ".ogg"):
		return mediaKindVoice
	case strings.HasPrefix(mimeType, "audio/"):
		return mediaKindAudio
	default:
		return mediaKindDocument
	}
}

func applyMediaAttachment(msg *Message, attachment mediaAttachment) {
	switch attachment.kind {
	case mediaKindPhoto:
		msg.Photo = &Photo{File: attachment.file}
	case mediaKindVideo:
		msg.Video = &Video{
			File:     attachment.file,
			MIME:     attachment.mimeType,
			FileName: attachment.fileName,
		}
	case mediaKindAnimation:
		msg.Animation = &Animation{
			File:     attachment.file,
			MIME:     attachment.mimeType,
			FileName: attachment.fileName,
		}
	case mediaKindAudio:
		msg.Audio = &Audio{
			File:     attachment.file,
			MIME:     attachment.mimeType,
			FileName: attachment.fileName,
		}
	case mediaKindVoice:
		msg.Voice = &Voice{
			File: attachment.file,
			MIME: attachment.mimeType,
		}
	default:
		msg.Document = &Document{
			File:     attachment.file,
			MIME:     attachment.mimeType,
			FileName: attachment.fileName,
		}
	}
}

func (self *MessageBuilder) applyCaption(msg *Message, caption string) {
	if caption == "" {
		return
	}

	msg.Caption = caption
	msg.Text = ""

	if !self.Mdv2Enabled && len(self.entities) > 0 {
		msg.CaptionEntities = self.entities
		msg.Entities = nil
	}

	switch {
	case msg.Photo != nil:
		msg.Photo.Caption = caption
	case msg.Video != nil:
		msg.Video.Caption = caption
	case msg.Animation != nil:
		msg.Animation.Caption = caption
	case msg.Audio != nil:
		msg.Audio.Caption = caption
	case msg.Voice != nil:
		msg.Voice.Caption = caption
	case msg.Document != nil:
		msg.Document.Caption = caption
	}
}

// AddFile attaches a Telegram file by cloud file_id with a local disk fallback.
// mimeType is used to pick the correct media kind; when empty it is inferred from fallbackPath.
func (self *MessageBuilder) AddFile(fileID, fallbackPath, mimeType string) *MessageBuilder {
	file := resolveBuilderFile(fileID, fallbackPath)
	if file.FileID == "" && file.FileLocal == "" {
		return self
	}

	fileName := filepath.Base(fallbackPath)
	if fileName == "." || fileName == "/" {
		fileName = ""
	}
	if mimeType == "" {
		mimeType = mimeTypeFromExtension(fileName)
	}

	self.files = append(self.files, mediaAttachment{
		file:     file,
		mimeType: mimeType,
		fileName: fileName,
		kind:     categorizeMediaFile(mimeType, fileName),
	})

	return self
}

// MirrorFileAsset describes a static media asset that may need per-mirror file_id caching.
type MirrorFileAsset struct {
	PrimaryFileID string
	FallbackPath  string
	MimeType      string
	MirrorFileKey string
}
