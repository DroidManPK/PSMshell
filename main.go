package main

import (
	"bufio"
	//"io/ioutil"
	"fmt"
	"github.com/pkg/term"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"errors"
	"strings"
	"syscall"
	"unsafe"

)

type Command string
type ParsedCommand struct {
	Args   []string
	Stdin  string
	Stdout string
}

var terminal *term.Term

var processGroups []uint32

var ForegroundPid uint32
var ForegroundProcess error = errors.New("")

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
	//Start Shell processing
	clrscr()
	fmt.Println("\n\n\t\t\tWelcome to PSM Shell\n\n\t\t\tAn Interactive Shell based on POSIX Standards\n\n\t\t\tDeveloped by Pavan Sanath Mandar\n\n\t\t\tVersion 2.0\n")
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
				if err == ForegroundProcess {
					terminal.Wait(child,processGroups,ForegroundPid)
				} //else if err != nil {
					//fmt.Fprintf(os.Stderr, "", err)
				//}
				PrintPrompt()
			}
			cmd = ""
		case '\u2191':
			PrintPrompt()
		case '\u007f', '\u0008'://                 Handle Backspace and delete keys
			if len(cmd) > 0 {
				cmd = cmd[:len(cmd)-1]    // Delete last char
				fmt.Printf("\u0008 \u0008")
			}
		case '\u0004':
			if len(cmd) == 0 {
				os.Exit(0)
			}
			err := cmd.Complete()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
		case '\t':
			err := cmd.Complete()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
		default:
			fmt.Printf("%c", c)
			cmd += Command(c)
		}
	}
}//End of main

func clrscr(){
	cmdout,_ :=exec.Command("clear").Output()
	fmt.Println(string(cmdout))
}

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
	case "&":
		background(c)
		return nil

	case "about":
		fmt.Println("\n\n\t\t\t\t\tPSMshell\n\n\t\t\t\t\t\t\tVersion 2.0\n\n\n\t\t\tShell Interpreter Designed and Implemented in Golang\n\n\n\t\t\tMain Features : \n\n\t\t\tFast and Quick Response due to Go code Optimization\n\n\t\t\tBackground Processing using Goroutines\n\n\t\t\tCommand and File name completion and suggesions\n\n\t\t\tBatch Procesing with the help of Pipes and Go routines\n\n\n\tConcieved and Developed by : -\n\n\tPavan Keshav L\n\tSanath P Holla\n\tMandar M Patil")
		return nil
	//default:
		//cmdout,er :=exec.Command(parsed[0], args[0:]...).Output()
		//fmt.Println(string(cmdout))

		//return er
	}

	var parsedtokens []Token = []Token{Token(parsed[0])}
	for _, t := range args {
		parsedtokens = append(parsedtokens, Token(t))
	}
	commands := ParseCommands(parsedtokens)

	var cmds []*exec.Cmd
	for i, c := range commands {
		if len(c.Args) == 0 {
			// This should have never happened, there is
			// no command, but let's avoid panicing.
			continue
		}
		newCmd := exec.Command(c.Args[0], c.Args[1:]...)
		newCmd.Stderr = os.Stderr
		cmds = append(cmds, newCmd)

		// If there was an Stdin specified, use it.
		if c.Stdin != "" {
			// Open the file to convert it to an io.Reader
			if f, err := os.Open(c.Stdin); err == nil {
				newCmd.Stdin = f
				defer f.Close()
			}
		} else {
			// There was no Stdin specified, so
			// connect it to the previous process in the
			// pipeline if there is one, the first process
			// still uses os.Stdin
			if i > 0 {
				pipe, err := cmds[i-1].StdoutPipe()
				if err != nil {
					continue
				}
				newCmd.Stdin = pipe
			} else {
				newCmd.Stdin = os.Stdin
			}
		}
		// If there was a Stdout specified, use it.
		if c.Stdout != "" {
			// Create the file to convert it to an io.Reader
			if f, err := os.Create(c.Stdout); err == nil {
				newCmd.Stdout = f
				defer f.Close()
			}
		} else {
			// There was no Stdout specified, so
			// connect it to the previous process in the
			// unless it's the last command in the pipeline,
			// which still uses os.Stdout
			if i == len(commands)-1 {
				newCmd.Stdout = os.Stdout
			}
		}
	}

	var pgrp uint32
	sysProcAttr := &syscall.SysProcAttr{
		Setpgid: true,
	}

	for _, c := range cmds {
		c.SysProcAttr = sysProcAttr
		if err := c.Start(); err != nil {
			return err
		}
		if sysProcAttr.Pgid == 0 {
			sysProcAttr.Pgid, _ = syscall.Getpgid(c.Process.Pid)
			pgrp = uint32(sysProcAttr.Pgid)
			processGroups = append(processGroups, uint32(c.Process.Pid))
		}
	}

	ForegroundPid = pgrp
	terminal.Restore()
	_, _, err1 := syscall.RawSyscall(
		syscall.SYS_IOCTL,
		uintptr(0),
		uintptr(syscall.TIOCSPGRP),
		uintptr(unsafe.Pointer(&pgrp)),
	)
	// RawSyscall returns an int for the error, we need to compare
	// to syscall.Errno(0) instead of nil
	if err1 != syscall.Errno(0) {
		return err1
	}
	return ForegroundProcess



}

func background(com Command) {
	var done Command
	done=Command("")
	for i, chr := range com{
		if(i>=2){done+=Command(chr)}
		}

	done+=Command(" >bg_op.txt")

	go done.HandleCmd()

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

func ParseCommands(tokens []Token) []ParsedCommand {
	// Keep track of the current command being built
	var currentCmd ParsedCommand
	// Keep array of all commands that have been built, so we can create the
	// pipeline
	var allCommands []ParsedCommand
	// Keep track of where this command started in parsed, so that we can build
	// currentCommand.Args when we find a special token.
	var lastCommandStart = 0
	// Keep track of if we've found a special token such as < or >, so that
	// we know if currentCmd.Args has already been populated.
	var foundSpecial bool
	var nextStdin, nextStdout bool
	for i, t := range tokens {
		if nextStdin {
			currentCmd.Stdin = string(t)
			nextStdin = false
		}
		if nextStdout {
			currentCmd.Stdout = string(t)
			nextStdout = false
		}
		if t.IsSpecial() || i == len(tokens)-1 {
			if foundSpecial == false {
				// Convert from Token to string
				var slice []Token
				if i == len(tokens)-1 {
					slice = tokens[lastCommandStart:]
				} else {
					slice = tokens[lastCommandStart:i]
				}

				for _, t := range slice {
					currentCmd.Args = append(currentCmd.Args, string(t))
				}
			}
			foundSpecial = true
		}
		if t.IsStdinRedirect() {
			nextStdin = true
		}
		if t.IsStdoutRedirect() {
			nextStdout = true
		}
		if t.IsPipe() || i == len(tokens)-1 {
			allCommands = append(allCommands, currentCmd)
			lastCommandStart = i + 1
			foundSpecial = false
			currentCmd = ParsedCommand{}
		}
	}
	return allCommands
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
