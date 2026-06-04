package config

type Fixture struct {
	Text   string
	Raw    string
	Config *Config
}

var fixtures = []*Fixture{
	{
		Raw:    "",
		Text:   "",
		Config: New(),
	},
	{
		Raw:    ";Comments only",
		Text:   "",
		Config: New(),
	},
	{
		Raw:    "#Comments only",
		Text:   "",
		Config: New(),
	},
	{
		Raw:    "[core]\nrepositoryformatversion=0",
		Text:   "[core]\n\trepositoryformatversion = 0\n",
		Config: New().AddOption("core", "", "repositoryformatversion", "0"),
	},
	{
		Raw:    "[core]\n\trepositoryformatversion = 0\n",
		Text:   "[core]\n\trepositoryformatversion = 0\n",
		Config: New().AddOption("core", "", "repositoryformatversion", "0"),
	},
	{
		Raw:    ";Commment\n[core]\n;Comment\nrepositoryformatversion = 0\n",
		Text:   "[core]\n\trepositoryformatversion = 0\n",
		Config: New().AddOption("core", "", "repositoryformatversion", "0"),
	},
	{
		Raw:    "#Commment\n#Comment\n[core]\n#Comment\nrepositoryformatversion = 0\n",
		Text:   "[core]\n\trepositoryformatversion = 0\n",
		Config: New().AddOption("core", "", "repositoryformatversion", "0"),
	},
	{
		Raw: `[section]
	option1 = "has # hash"
	option2 = "has \" quote"
	option3 = "has \\ backslash"
	option4 = "has ; semicolon"
	option5 = "has \n line-feed"
	option6 = "has \t tab"
	option7 = "  has leading spaces"
	option8 = "has trailing spaces  "
	option9 = has no special characters
	option10 = has unusual ` + "\x01\x7f\xc8\x80 characters\n",
		Text: `[section]
	option1 = "has # hash"
	option2 = "has \" quote"
	option3 = "has \\ backslash"
	option4 = "has ; semicolon"
	option5 = "has \n line-feed"
	option6 = "has \t tab"
	option7 = "  has leading spaces"
	option8 = "has trailing spaces  "
	option9 = has no special characters
	option10 = has unusual ` + "\x01\x7f\xc8\x80 characters\n",
		Config: New().
			AddOption("section", "", "option1", `has # hash`).
			AddOption("section", "", "option2", `has " quote`).
			AddOption("section", "", "option3", `has \ backslash`).
			AddOption("section", "", "option4", `has ; semicolon`).
			AddOption("section", "", "option5", "has \n line-feed").
			AddOption("section", "", "option6", "has \t tab").
			AddOption("section", "", "option7", `  has leading spaces`).
			AddOption("section", "", "option8", `has trailing spaces  `).
			AddOption("section", "", "option9", `has no special characters`).
			AddOption("section", "", "option10", "has unusual \x01\x7f\u0200 characters"),
	},
	{
		Raw: `
			[sect1]
			opt1 = value1
			[sect1 "subsect1"]
			opt2 = value2
		`,
		Text: `[sect1]
	opt1 = value1
[sect1 "subsect1"]
	opt2 = value2
`,
		Config: New().
			AddOption("sect1", "", "opt1", "value1").
			AddOption("sect1", "subsect1", "opt2", "value2"),
	},
	{
		Raw: `
			[sect1]
			opt1 = value1
			[sect1 "subsect1"]
			opt2 = value2
			[sect1]
			opt1 = value1b
			[sect1 "subsect1"]
			opt2 = value2b
			[sect1 "subsect2"]
			opt2 = value2
		`,
		Text: `[sect1]
	opt1 = value1
	opt1 = value1b
[sect1 "subsect1"]
	opt2 = value2
	opt2 = value2b
[sect1 "subsect2"]
	opt2 = value2
`,
		Config: New().
			AddOption("sect1", "", "opt1", "value1").
			AddOption("sect1", "", "opt1", "value1b").
			AddOption("sect1", "subsect1", "opt2", "value2").
			AddOption("sect1", "subsect1", "opt2", "value2b").
			AddOption("sect1", "subsect2", "opt2", "value2"),
	},
	{
		Raw: `
			[sect1]
			opt1 = value1
			opt1 = value2
			`,
		Text: `[sect1]
	opt1 = value1
	opt1 = value2
`,
		Config: New().
			AddOption("sect1", "", "opt1", "value1").
			AddOption("sect1", "", "opt1", "value2"),
	},
}
