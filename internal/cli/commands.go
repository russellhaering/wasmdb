package cli

// command represents a registered CLI command.
type command struct {
	// noun is the top-level noun (e.g. "db", "doc", "search").
	noun string
	// verb is the sub-command (e.g. "list", "create"). Empty for top-level commands.
	verb string
	// usage is a one-line usage string.
	usage string
	// description is a short description for help output.
	description string
	// run executes the command.
	run func(ctx *cmdContext) error
}

// commands is the global command registry.
var commands []command

func register(c command) {
	commands = append(commands, c)
}

// findCommand finds a command by noun and verb.
func findCommand(noun, verb string) *command {
	for i := range commands {
		if commands[i].noun == noun && commands[i].verb == verb {
			return &commands[i]
		}
	}
	return nil
}

// commandsForNoun returns all commands with the given noun.
func commandsForNoun(noun string) []command {
	var result []command
	for _, c := range commands {
		if c.noun == noun {
			result = append(result, c)
		}
	}
	return result
}
