package app

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
)

// xlsxCell is a cell of a sheet as Excel reads it.
type xlsxCell struct {
	S  string `xml:"s,attr"`
	T  string `xml:"t,attr"`
	V  string `xml:"v"`
	Is string `xml:"is>t"`
}

// readXLSX opens path as Excel would: a zip whose every part is well-formed XML,
// the parts a workbook has, and the sheet's rows of cells.
func readXLSX(t *testing.T, path string) (sheet string, rows [][]xlsxCell, frozen bool) {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		r.Close()
		d := xml.NewDecoder(strings.NewReader(string(data)))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is no XML: %v", f.Name, err)
			}
		}
		switch f.Name {
		case "xl/workbook.xml":
			var wb struct {
				Sheets []struct {
					Name string `xml:"name,attr"`
				} `xml:"sheets>sheet"`
			}
			if err := xml.Unmarshal(data, &wb); err != nil || len(wb.Sheets) != 1 {
				t.Fatalf("workbook %s %v", data, err)
			}
			sheet = wb.Sheets[0].Name
		case "xl/worksheets/sheet1.xml":
			var ws struct {
				Pane struct {
					State string `xml:"state,attr"`
				} `xml:"sheetViews>sheetView>pane"`
				Rows []struct {
					Cells []xlsxCell `xml:"c"`
				} `xml:"sheetData>row"`
			}
			if err := xml.Unmarshal(data, &ws); err != nil {
				t.Fatal(err)
			}
			frozen = ws.Pane.State == "frozen"
			for _, r := range ws.Rows {
				rows = append(rows, r.Cells)
			}
		}
	}
	for _, want := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
		if !slices.Contains(names, want) {
			t.Errorf("no %s in %v", want, names)
		}
	}
	return sheet, rows, frozen
}

// A page exported to Excel: the columns as shown, the header bold and frozen, a
// number column's values numbers, the rest text with what XML cannot hold left
// out and a tab kept, NULL an empty cell, the sheet named after the table
// (§7.6「导出」).
func TestExportExcel(t *testing.T) {
	dir := inDir(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 4)
	tab.hidden["meta"], tab.hidden["created_at"] = true, true
	exportAt(t, a, exportXLSXPage)
	path := filepath.Join(dir, "t_order.xlsx")
	if !strings.Contains(a.toast, path) || !strings.HasSuffix(a.toast, " · 4 行") {
		t.Fatalf("toast %q", a.toast)
	}
	sheet, rows, frozen := readXLSX(t, path)
	if sheet != "t_order" || !frozen || len(rows) != 5 {
		t.Fatalf("sheet %q, frozen %v, %d rows", sheet, frozen, len(rows))
	}
	text := func(c xlsxCell) string { return c.Is + c.V }
	var head []string
	for _, c := range rows[0] {
		head = append(head, text(c))
		if c.S != "1" {
			t.Errorf("header cell %q not bold", text(c))
		}
	}
	if strings.Join(head, ",") != "id,status,amount,paid,note" {
		t.Errorf("header %v", head)
	}
	id, amount, note := rows[1][0], rows[1][2], rows[2][4]
	if id.T != "" || id.V != "1" || amount.V != "1.99" || rows[1][1].T != "inlineStr" || rows[1][1].Is != "running" {
		t.Errorf("row 1: %+v", rows[1])
	}
	if note.Is != "line one\nline two\tafter tab [31mred" { // ESC is no XML character
		t.Errorf("note %q", note.Is)
	}
	if c := rows[3][4]; c.Is != "" || c.V != "" || c.T != "" {
		t.Errorf("a NULL: %+v", c)
	}
}

// Numbers Excel would round are text, every digit kept; what is no number is
// text too; Excel's last row ends the sheet as a budget does.
func TestXLSXCells(t *testing.T) {
	for s, want := range map[string]bool{"1": true, "-0.25": true, "1.5e10": true, "123456789012345": true,
		"1234567890123456": false, "12345678901234567890123456789.123": false, "NaN": false, "Inf": false, "0x1F": false, "1_000": false, "abc": false, "": false} {
		if exactNumber(s) != want {
			t.Errorf("exactNumber(%q) = %v", s, !want)
		}
	}
	if sheetName("a/b:c[1]?") != "a_b_c_1__" || len([]rune(sheetName(strings.Repeat("x", 40)))) != 31 || sheetName("") != "Sheet1" {
		t.Error("sheet names")
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "x.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	o := &exportOut{f: f, w: bufio.NewWriter(f), want: []exportCol{{name: "n", typ: "bigint"}}, xlsx: &xlsxOut{}, sheet: "x"}
	cols := []db.Col{{Name: "n"}}
	for range 2 { // a request run again starts the file over
		if err := o.head(cols); err != nil {
			t.Fatal(err)
		}
		_ = o.row([]db.Val{{S: "7"}})
	}
	o.xlsx.rows = xlsxRows // at Excel's last row
	if err := o.row([]db.Val{{S: "8"}}); !errors.Is(err, db.ErrTruncated) {
		t.Errorf("past the last row: %v", err)
	}
	o.xlsx.rows = 2
	if err := o.finish(); err != nil {
		t.Fatal(err)
	}
	_, rows, _ := readXLSX(t, f.Name())
	if len(rows) != 2 || rows[1][0].V != "7" || o.class[0] != ui.ColNumber {
		t.Errorf("rows %+v", rows)
	}
}
