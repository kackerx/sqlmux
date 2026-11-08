package app

import (
	"archive/zip"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
)

// An Excel export (§7.6「导出」): an .xlsx is a zip of a few XML parts, which
// the standard library writes; the sheet's rows go into the zip as they come,
// as a CSV's go into its file. Its header row is bold and frozen; a number
// column's value that Excel holds exactly is a number cell, any other a text
// one, NULL an empty one.

// Excel's limits: rows in a sheet, characters in a cell.
const (
	xlsxRows     = 1 << 20
	xlsxCellText = 32767
)

// xlsxParts are the workbook's parts but its sheet, the same for every file.
var xlsxParts = []struct{ name, body string }{
	{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`},
	{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
	{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
	{"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs></styleSheet>`},
}

// xlsxOut is an export's workbook on its way: the sheet's part open in the
// zip, rows written into it.
type xlsxOut struct {
	z     *zip.Writer
	sheet io.Writer
	rows  int
}

// start writes the parts but the sheet, the workbook naming its sheet name,
// and opens the sheet, its header row of names written, frozen.
func (x *xlsxOut) start(w io.Writer, name string, names []string) error {
	x.z, x.rows = zip.NewWriter(w), 0
	for _, p := range xlsxParts {
		if err := x.part(p.name, p.body); err != nil {
			return err
		}
	}
	if err := x.part("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="`+xmlText(sheetName(name))+`" sheetId="1" r:id="rId1"/></sheets></workbook>`); err != nil {
		return err
	}
	var err error
	if x.sheet, err = x.z.Create("xl/worksheets/sheet1.xml"); err != nil {
		return err
	}
	if _, err := io.WriteString(x.sheet, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetData>`); err != nil {
		return err
	}
	head := make([]db.Val, len(names))
	for i, n := range names {
		head[i] = db.Val{S: n}
	}
	return x.row(head, nil, 1)
}

func (x *xlsxOut) part(name, body string) error {
	w, err := x.z.Create(name)
	if err == nil {
		_, err = io.WriteString(w, body)
	}
	return err
}

// row writes vals, a number column's as a number cell where Excel holds it
// exactly, the rest as text, in style; past Excel's last row it writes nothing
// and says the export is cut (db.ErrTruncated).
func (x *xlsxOut) row(vals []db.Val, class []ui.ColType, style int) error {
	if x.rows == xlsxRows {
		return db.ErrTruncated
	}
	x.rows++
	var b strings.Builder
	b.WriteString("<row>")
	s := ""
	if style > 0 {
		s = ` s="` + strconv.Itoa(style) + `"`
	}
	for i, v := range vals {
		switch {
		case v.Null:
			b.WriteString("<c" + s + "/>")
		case i < len(class) && class[i] == ui.ColNumber && exactNumber(v.S):
			b.WriteString("<c" + s + "><v>" + v.S + "</v></c>")
		default:
			b.WriteString(`<c` + s + ` t="inlineStr"><is><t xml:space="preserve">` + xmlText(runesUpTo(v.S, xlsxCellText)) + "</t></is></c>")
		}
	}
	b.WriteString("</row>")
	_, err := io.WriteString(x.sheet, b.String())
	return err
}

// finish closes the sheet and the zip.
func (x *xlsxOut) finish() error {
	if _, err := io.WriteString(x.sheet, "</sheetData></worksheet>"); err != nil {
		return err
	}
	return x.z.Close()
}

// exactNumber is whether s is a decimal number Excel keeps as it is: 15
// significant digits at most, as its doubles do; a longer one (a NUMBER(38)'s
// id) is text, its digits kept.
func exactNumber(s string) bool {
	if _, err := strconv.ParseFloat(s, 64); err != nil || strings.ContainsAny(s, "xXnNiI_") { // no hex, NaN, Inf, 1_000
		return false
	}
	mant, _, _ := strings.Cut(strings.ToLower(s), "e")
	digits := strings.TrimLeft(strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, mant), "0")
	return len(digits) <= 15
}

// runesUpTo is s to n characters at most.
func runesUpTo(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// xmlText is s escaped for XML text, the characters XML 1.0 has no place for
// left out (NUL and the other controls but tab, newline and return).
func xmlText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r', r == 0xFFFE, r == 0xFFFF, r == utf8.RuneError:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sheetName is name as a sheet's: 31 characters at most, none of []:*?/\.
func sheetName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '_'
		}
		return r
	}, name)
	if name = runesUpTo(name, 31); name == "" {
		return "Sheet1"
	}
	return name
}
