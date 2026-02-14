package bot

type Localization struct {
	WarningText  string
	WelcomeText  string
	JoinText     string
	NotFoundText string
}

var (
	localization = Localization{
		WarningText:  "⚠️ فایل‌ها بعد از ۳۰ ثانیه حذف خواهند شد",
		WelcomeText:  "سلام! برای دانلود روی لینک فایل کلیک کنید.",
		JoinText:     "لطفاً ابتدا در کانال‌های زیر عضو شوید:",
		NotFoundText: "فایل پیدا نشد یا لینک منقضی شده است.",
	}
)

func (l Localization) WithWarning(text string) Localization {
	if text == "" {
		return l
	}
	l.WarningText = text
	return l
}

func (l Localization) WithWelcome(text string) Localization {
	if text == "" {
		return l
	}
	l.WelcomeText = text
	return l
}

func (l Localization) WithJoin(text string) Localization {
	if text == "" {
		return l
	}
	l.JoinText = text
	return l
}

func (l Localization) WithNotFound(text string) Localization {
	if text == "" {
		return l
	}
	l.NotFoundText = text
	return l
}

func (b *Bot) Localization() Localization {
	return localization
}
