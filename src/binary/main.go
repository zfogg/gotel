package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usage = `Usage:
  gotel sql format [--dialect generic|postgresql|sqlite|mysql|duckdb|clickhouse] [SQL]
  gotel sql format [--dialect DIALECT] --edit-file PATH
  gotel sql sanitize [--dialect DIALECT] [SQL]

SQL is read from stdin when omitted. Output goes to stdout.
--edit-file conservatively collapses horizontal spacing between SQL tokens,
preserving comments, literals, indentation, blank lines and line endings.
`

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(stdout, usage)
		return err
	}
	if len(args) < 2 || args[0] != "sql" || (args[1] != "format" && args[1] != "sanitize") {
		return fmt.Errorf("%s", usage)
	}
	flags := flag.NewFlagSet("sql "+args[1], flag.ContinueOnError)
	flags.SetOutput(stderr)
	dialect := flags.String("dialect", "generic", "SQL dialect")
	var path string
	if args[1] == "format" {
		flags.StringVar(&path, "edit-file", "", "format a file in place")
	}
	if err := flags.Parse(args[2:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() > 1 || (path != "" && flags.NArg() != 0) {
		return fmt.Errorf("provide one SQL argument or --edit-file PATH")
	}
	var input []byte
	var err error
	if path != "" {
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("--edit-file requires a regular file")
		}
		input, err = os.ReadFile(path)
	} else if flags.NArg() == 1 {
		input = []byte(flags.Arg(0))
	} else {
		input, err = io.ReadAll(io.LimitReader(stdin, 1024*1024+1))
	}
	if err != nil {
		return err
	}
	result, err := transform(string(input), args[1], strings.ToLower(*dialect), path != "")
	if err != nil {
		return err
	}
	if path != "" {
		if bytes.Equal(input, []byte(result)) {
			return nil
		}
		return replaceFile(path, []byte(result))
	}
	_, err = io.WriteString(stdout, result)
	return err
}

// Write a complete sibling before replacing the original, so formatting and
// write errors leave the original intact. Preserve the file's permission bits.
func replaceFile(path string, content []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gotel-sql-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		_, err = f.Write(content)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
