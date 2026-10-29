package expand

// Rewrite styles (PRD Appendix 2, FR-8): who the rewrite's wording is
// tuned for. The instructions were settled by evaluation (Appendix 2,
// 2.6); each line is there because a looser one failed.
const (
	StyleDefault = "default" // DefaultSystem: the model decides
	StyleLLM     = "llm"     // for the assistant: direct and terse
	StyleHuman   = "human"   // for people: natural, in the developer's voice
	StyleBoth    = "both"    // natural, structured when there are several parts
)

// Styles are the values of expand.style.
var Styles = []string{StyleDefault, StyleLLM, StyleHuman, StyleBoth}

// styleRules are kept by every style but StyleDefault (FR-8.2). The last
// rule is there because "conversational" gave "Can you…?" requests, which
// Guard stops as the model asking a question.
const styleRules = `Rules:
- Keep the developer's intent exactly. Never add goals, components, files, or steps they didn't mention.
- Keep names exactly as written: agents, skills, commands, @mentions, files, paths, branches, and issue, MR or PR numbers. A word with a hyphen, slash, dot, @, #, ! or digits is a name: never split, translate or drop it.
- Keep every limit and negation ("don't", "only", "without", "never").
- If the request is vague, keep it vague: do not guess what "it" refers to.
- If the developer asks a question, keep it a question.
- Keep the developer's language.
- Fix typos and spell out abbreviations.
- A request stays a request: write it as a statement, not as a "Can you…?" question. Never add a question, a greeting or a closing line.`

const styleEnd = `
Output only the rewritten prompt, under 60 words.`

// In testing, a fixed "Constraint:" line made the model invent
// constraints, "precise technical terms" turned "using code-reviewer"
// into "review the code", and an imperative report ("it still fails")
// became a task.
const llmSystem = `You rewrite a developer's short, rough request to an AI coding assistant (Claude Code) into a prompt tuned for the assistant to act on.
` + styleRules + `
Style:
- Imperative, addressed to the assistant. Neutral and direct: no greetings, politeness or filler.
- Goal first, then details, then what must not be done, if the developer said so. Never add a constraint they didn't state.
- Use the developer's terms; "using X" means "Use X".
- Only a request becomes an instruction. A report ("it still fails") or a question stays as it is.
- For several parts, one bullet per part they mentioned.` + styleEnd

const humanSystem = `You rewrite a developer's short, rough request to an AI coding assistant (Claude Code) so it reads naturally to a person.
` + styleRules + `
Style:
- Natural, as written to a colleague, in the developer's voice. Polite only if the developer was.
- Keep the developer's order of ideas.
- Everyday words, short sentences.
- Sentences, not bullets, unless the developer listed several items.` + styleEnd

const bothSystem = `You rewrite a developer's short, rough request to an AI coding assistant (Claude Code) so it reads naturally to a person and is clear for the assistant.
` + styleRules + `
Style:
- Written as the developer speaking: natural, short sentences, everyday words. Polite only if the developer was.
- A single request stays one or two sentences.
- For several parts: the goal first, then one bullet per part they mentioned, then what must not be done.` + styleEnd

// StyleSystem returns the instructions for style; DefaultSystem for
// StyleDefault, "" or an unknown style.
func StyleSystem(style string) string {
	switch style {
	case StyleLLM:
		return llmSystem
	case StyleHuman:
		return humanSystem
	case StyleBoth:
		return bothSystem
	}
	return DefaultSystem
}
