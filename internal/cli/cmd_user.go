package cli

import "fmt"

func init() {
	register(command{
		noun:        "user",
		verb:        "create",
		usage:       "wasmdb user create --email <email> --password <password> [--json]",
		description: "Create a new user",
		run:         userCreate,
	})
	register(command{
		noun:        "user",
		verb:        "list",
		usage:       "wasmdb user list [--json]",
		description: "List all users",
		run:         userList,
	})
}

func userCreate(ctx *cmdContext) error {
	email := ctx.flag("email")
	password := ctx.flag("password")

	if email == "" {
		return fmt.Errorf("--email is required")
	}
	if password == "" {
		return fmt.Errorf("--password is required")
	}

	user, err := ctx.backend.CreateUser(ctx, email, password)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, user)
	}
	fmt.Fprintf(ctx.stdout, "id: %s\nemail: %s\n", user.ID, user.Email)
	return nil
}

func userList(ctx *cmdContext) error {
	users, err := ctx.backend.ListUsers(ctx)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, users)
	}

	if len(users) == 0 {
		fmt.Fprintln(ctx.stdout, "no users")
		return nil
	}

	for _, u := range users {
		fmt.Fprintf(ctx.stdout, "%s\t%s\n", u.ID, u.Email)
	}
	return nil
}
