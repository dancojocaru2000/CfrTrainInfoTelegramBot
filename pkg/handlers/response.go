package handlers

import "github.com/go-telegram/bot"

type HandlerResponse struct {
	Message                 *bot.SendMessageParams
	ProgressMessageToEditId int
	CallbackAnswer          *bot.AnswerCallbackQueryParams
	MessageEdits            []*bot.EditMessageTextParams
	MessageMarkupEdits      []*bot.EditMessageReplyMarkupParams
	ShouldUnsubscribe       bool
	Injected                struct {
		ChatId    int64
		MessageId int
	}
}
