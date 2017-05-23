package base

import "fmt"

const (
	// TextBlack black
	TextBlack = iota + 30
	// TextRed red
	TextRed
	// TextGreen green
	TextGreen
	// TextYellow yellow
	TextYellow
	// TextBlue blue
	TextBlue
	// TextMagenta magenta
	TextMagenta
	// TextCyan cyan
	TextCyan
	// TextWhite whilte
	TextWhite
)

// Black black string
func Black(str string) string {
	return textColor(TextBlack, str)
}

func Red(str string) string {
	return textColor(TextRed, str)
}
func RedF(str string, a ...interface{}) string {
	return fmt.Sprintf(textColor(TextRed, str), a)
}

func Green(str string) string {
	return textColor(TextGreen, str)
}
func GreenF(str string, a ...interface{}) string {
	return fmt.Sprintf(textColor(TextGreen, str), a)
}
func Yellow(str string) string {
	return textColor(TextYellow, str)
}
func YellowF(str string, a ...interface{}) string {
	return fmt.Sprintf(textColor(TextYellow, str), a)
}
func Blue(str string) string {
	return textColor(TextBlue, str)
}
func BlueF(str string, a ...interface{}) string {
	return fmt.Sprintf(textColor(TextBlue, str), a)
}
func Magenta(str string) string {
	return textColor(TextMagenta, str)
}

func Cyan(str string) string {
	return textColor(TextCyan, str)
}

func White(str string) string {
	return textColor(TextWhite, str)
}
