package telegram

// SendMessageRequest is the JSON body for Bot API sendMessage.
type SendMessageRequest struct {
	ChatID      string       `json:"chat_id"`
	Text        string       `json:"text"`
	ParseMode   string       `json:"parse_mode,omitempty"`
	ReplyMarkup *ReplyMarkup `json:"reply_markup,omitempty"`
}

// ReplyMarkup contains inline keyboard markup.
type ReplyMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton is a URL button (NOT callback_data).
type InlineKeyboardButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// SendMessageResponse is the Bot API response.
type SendMessageResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}
