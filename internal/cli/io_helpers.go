package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/olekukonko/tablewriter"
)

func stderrf(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stderr, format, args...); err != nil {
		return
	}
}

func stderrln(args ...any) {
	if _, err := fmt.Fprintln(os.Stderr, args...); err != nil {
		return
	}
}

func stdoutf(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stdout, format, args...); err != nil {
		return
	}
}

func printfOut(format string, args ...any) {
	if _, err := fmt.Printf(format, args...); err != nil {
		return
	}
}

func printOut(args ...any) {
	if _, err := fmt.Print(args...); err != nil {
		return
	}
}

func printlnOut(args ...any) {
	if _, err := fmt.Println(args...); err != nil {
		return
	}
}

func waitForEnter() {
	if _, err := fmt.Scanln(); err != nil {
		return
	}
}

func closeBestEffort(c io.Closer) {
	if err := c.Close(); err != nil {
		return
	}
}

func appendTableRowBestEffort(table *tablewriter.Table, row []string) {
	if err := table.Append(row); err != nil {
		return
	}
}

func renderTableBestEffort(table *tablewriter.Table) {
	if err := table.Render(); err != nil {
		return
	}
}
