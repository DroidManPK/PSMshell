package main

import (
	"bufio"

	"fmt"
	"github.com/pkg/term"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"

	"strings"
	"syscall"

)

type Command string
type ParsedCommand struct {
	Args   []string
	Stdin  string
	Stdout string
}

var terminal *term.Term

var homedirRe *regexp.Regexp = regexp.MustCompile("^~([a-zA-Z]*)?(/*)?")//set HomeDir pattern

func main() {
	// Initialize the terminal
	t, err := term.Open("/dev/tty")
	if err != nil {
		panic(err)
	}
	// Restore the previous terminal settings at the end of the program
	defer t.Restore()
	t.SetCbreak()
	terminal = t

	child := make(chan os.Signal) //make a channel!
	signal.Notify(child, syscall.SIGCHLD)
	signal.Ignore(
		syscall.SIGTTOU,  //Orphaned process signal ignored
		syscall.SIGINT, // Signal Interrupt Ignored
	)
	os.Setenv("$", "$")//Set initial env values
	os.Setenv("SHELL", os.Args[0])
	if u, err := user.Current(); err == nil {
		SourceFile(u.HomeDir + "/.shrc")//Load Profile file of terminal
	}
	PrintPrompt()
	r := bufio.NewReader(t)
	var cmd Command
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		switch c {
		case '\n':

			//  print the newline itself to the terminal.
			fmt.Printf("\n")

			if cmd == "exit" || cmd == "quit" {
				t.Restore()//                             Restore settings and quit
				os.Exit(0)
			} else if cmd == "" {
				PrintPrompt()
			} else {
				err := cmd.HandleCmd()
				if err != nil {
					fmt.Fprintf(os.Stderr, "%v\n", err)
				}
				PrintPrompt()
			}
			cmd = ""

		case '\u007f', '\u0008'://                 Handle Backspace and delete keys
			if len(cmd) > 0 {
				cmd = cmd[:len(cmd)-1]    // Delete last char
				fmt.Printf("\u0008 \u0008")
			}

		default:
			fmt.Printf("%c", c)
			cmd += Command(c)
		}
	}
}//End of main

func (c Command) HandleCmd() error {
	parsed := c.Tokenize()
	if len(parsed) == 0 {
		// There was no command, it's not an error, the user just hit enter.
		PrintPrompt()
		return nil
	}
	args := make([]string, 0, len(parsed))
	for _, val := range parsed[1:] {
		args = append(args, os.ExpandEnv(val))
	}
	// newargs will be at least len(parsed in size, so start by allocating a slice of that capacity
	newargs := make([]string, 0, len(args))//finally get the actual arguments
	for _, token := range args {
		token = replaceTilde(token)//expand tilde to home dir
		expanded, err := filepath.Glob(token)//match all files matching pattern of token else return nil
		if err != nil || len(expanded) == 0 {
			newargs = append(newargs, token)
			continue
		}
		newargs = append(newargs, expanded...)

	}
	args = newargs

	switch parsed[0] {
	case "cd":
		if len(args) == 0 {
			return fmt.Errorf("Must provide an argument to cd")
		}
		old, _ := os.Getwd()
		err := os.Chdir(args[0])
		if err == nil {
			new, _ := os.Getwd()
			os.Setenv("PWD", new)
			os.Setenv("OLDPWD", old)
		}
		return err
	case "set":
		if len(args) != 2 {
			return fmt.Errorf("Usage: set var value")
		}
		return os.Setenv(args[0], args[1])

	default:

		cmdout,er :=exec.Command(parsed[0], parsed[1:]...).Output()
		fmt.Println(string(cmdout))

		return er
	}

}

func PrintPrompt() {
	if p := os.Getenv("PROMPT"); p != "" {
		if len(p) > 1 && p[0] == '!' {
			input := os.ExpandEnv(p[1:])
			split := strings.Fields(input)
			cmd := exec.Command(split[0], split[1:]...)
			cmd.Stdout = os.Stderr
			if err := cmd.Run(); err != nil {
				if _, ok := err.(*exec.ExitError); !ok {
					// Fall back on our standard prompt, with a warning.
					fmt.Fprintf(os.Stderr, "\nInvalid prompt command\n> ")
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "\n%s", os.ExpandEnv(p))
		}
	} else {
		fmt.Fprintf(os.Stderr, "\n%s> ",os.Getenv("PWD"))
	}
}

//Setup Terminal Profile
func SourceFile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewReader(f)
	for {
		line, err := scanner.ReadString('\n')
		switch err {
		case io.EOF:
			return nil
		case nil:
			// Nothing special
		default:
			return err
		}
		c := Command(line)
		if err := c.HandleCmd(); err != nil {
			return err
		}
	}
}


//Check if Directory is in User access to expand tilde
func replaceTilde(s string) string {
	if match := homedirRe.FindStringSubmatch(s); match != nil {
		var u *user.User
		var err error
		if match[1] != "" {
			u, err = user.Lookup(match[1])
		} else {
			u, err = user.Current()
		}
		if err == nil {
			return strings.Replace(s, match[0], u.HomeDir, 1)
		}
	}
	return s
}
