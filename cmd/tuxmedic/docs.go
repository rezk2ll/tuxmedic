package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

var (
	subcommands = []string{"ask", "check", "watch", "report", "list", "version", "help"}
	flagNames   = []string{"--plain", "--json", "--verbose", "--ascii", "--no-redact", "--for", "--version"}
)

func completion(w io.Writer, shell string, all []doctor.Check) error {
	ids := make([]string, len(all))
	for i, c := range all {
		ids[i] = c.ID
	}
	subs, flags, checks := strings.Join(subcommands, " "), strings.Join(flagNames, " "), strings.Join(ids, " ")
	switch shell {
	case "bash":
		fmt.Fprintf(w, `_tuxmedic() {
    local cur=${COMP_WORDS[COMP_CWORD]}
    if [[ $cur == -* ]]; then
        COMPREPLY=($(compgen -W "%s" -- "$cur"))
    elif [[ $COMP_CWORD -eq 1 ]]; then
        COMPREPLY=($(compgen -W "%s" -- "$cur"))
    elif [[ ${COMP_WORDS[1]} == check ]]; then
        COMPREPLY=($(compgen -W "%s" -- "$cur"))
    fi
}
complete -F _tuxmedic tuxmedic
`, flags, subs, checks)
	case "zsh":
		fmt.Fprintf(w, `#compdef tuxmedic
_tuxmedic() {
    if [[ $words[CURRENT] == -* ]]; then
        compadd -- %s
    elif (( CURRENT == 2 )); then
        compadd -- %s
    elif [[ $words[2] == check ]]; then
        compadd -- %s
    fi
}
_tuxmedic "$@"
`, flags, subs, checks)
	case "fish":
		fmt.Fprintf(w, "complete -c tuxmedic -f\n")
		fmt.Fprintf(w, "complete -c tuxmedic -n __fish_use_subcommand -a '%s'\n", subs)
		fmt.Fprintf(w, "complete -c tuxmedic -n '__fish_seen_subcommand_from check' -a '%s'\n", checks)
		for _, f := range flagNames {
			fmt.Fprintf(w, "complete -c tuxmedic -l %s\n", strings.TrimPrefix(f, "--"))
		}
	default:
		return fmt.Errorf("no completion for %q, pick bash, zsh or fish", shell)
	}
	return nil
}

// manPage writes tuxmedic(1) in roff, from the same usage text and check
// list the program prints, so the two never disagree.
func manPage(w io.Writer, all []doctor.Check) {
	esc := strings.NewReplacer(`\`, `\\`, "-", `\-`, "'", `\(aq`, "…", "...").Replace
	fmt.Fprintf(w, ".TH TUXMEDIC 1 %q \"tuxmedic %s\" \"User Commands\"\n", time.Now().UTC().Format("2006-01-02"), esc(versionString()))
	fmt.Fprintln(w, ".SH NAME\ntuxmedic \\- find out what is wrong with your Linux computer and how to fix it")
	fmt.Fprintln(w, ".SH SYNOPSIS\n.B tuxmedic\n[\\fICOMMAND\\fR] [\\fIOPTIONS\\fR]")
	fmt.Fprintln(w, ".SH DESCRIPTION\ntuxmedic checks the common causes of desktop Linux problems and explains each one in plain language, with the command that fixes it on your distro. Without a command it opens an interactive screen.")
	section := ""
	for _, line := range strings.Split(usage, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "Usage:":
			section = "COMMANDS"
			fmt.Fprintln(w, ".SH COMMANDS")
		case t == "Options:":
			section = "OPTIONS"
			fmt.Fprintln(w, ".SH OPTIONS")
		case strings.HasPrefix(t, "Exit status:"):
			fmt.Fprintln(w, ".SH EXIT STATUS\n"+esc(strings.TrimPrefix(t, "Exit status: ")))
		case section != "" && t != "":
			term, desc, _ := strings.Cut(t, "  ")
			fmt.Fprintf(w, ".TP\n.B %s\n%s\n", esc(term), esc(strings.TrimSpace(desc)))
		}
	}
	fmt.Fprintln(w, ".SH CHECKS")
	for _, c := range all {
		fmt.Fprintf(w, ".TP\n.B %s\n%s\n", c.ID, esc(c.Question))
	}
	fmt.Fprintln(w, ".SH PRIVACY\ntuxmedic only reads the computer. The only network traffic is a connection test and a name lookup. Nothing is sent anywhere. \\fBtuxmedic report\\fR removes names, addresses and serial numbers.")
	fmt.Fprintln(w, ".SH SEE ALSO\nhttps://github.com/rezk2ll/tuxmedic")
}
