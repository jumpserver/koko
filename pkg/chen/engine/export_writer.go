package engine

import (
	"archive/zip"
	"bufio"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const exportMaxCellChars = 32767

type exportRowWriter interface {
	Row([]any) error
	Close() error
}
type csvExportWriter struct {
	csv    *csv.Writer
	buffer *bufio.Writer
}

func newCSVExportWriter(w io.Writer, columns []Column) (exportRowWriter, error) {
	b := bufio.NewWriterSize(w, 64<<10)
	c := &csvExportWriter{csv: csv.NewWriter(b), buffer: b}
	headers := make([]any, len(columns))
	for i, v := range columns {
		headers[i] = v.Name
	}
	return c, c.Row(headers)
}
func (c *csvExportWriter) Row(row []any) error {
	out := make([]string, len(row))
	for i, value := range row {
		if value == nil {
			out[i] = `\N`
			continue
		}
		text, err := exportText(value)
		if err != nil {
			return err
		}
		if strings.HasPrefix(text, `\`) {
			text = `\` + text
		}
		out[i] = safeCSVValue(text)
	}
	return c.csv.Write(out)
}
func (c *csvExportWriter) Close() error {
	c.csv.Flush()
	if err := c.csv.Error(); err != nil {
		return err
	}
	return c.buffer.Flush()
}
func exportText(v any) (string, error) {
	if text, ok := v.(string); ok {
		return text, nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

// Minimal SpreadsheetML is streamed directly through ZIP. Inline strings avoid
// an unbounded shared-string table and prevent formulas/numeric precision loss.
// No workbook or complete worksheet is buffered in memory or re-read on download.
type xlsxExportWriter struct {
	zip   *zip.Writer
	sheet *bufio.Writer
	row   int
}

func newXLSXExportWriter(w io.Writer, columns []Column) (exportRowWriter, error) {
	if len(columns) > 16384 {
		return nil, &APIError{"export_column_limit", "XLSX supports at most 16,384 columns; choose CSV"}
	}
	z := zip.NewWriter(w)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Results" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
	}
	for _, part := range parts {
		entry, err := z.Create(part.name)
		if err != nil {
			return nil, err
		}
		if _, err = io.WriteString(entry, part.body); err != nil {
			return nil, err
		}
	}
	entry, err := z.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	x := &xlsxExportWriter{zip: z, sheet: bufio.NewWriterSize(entry, 64<<10)}
	if _, err = io.WriteString(x.sheet, `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetData>`); err != nil {
		return nil, err
	}
	headers := make([]any, len(columns))
	for i, c := range columns {
		headers[i] = c.Name
	}
	if err = x.Row(headers); err != nil {
		return nil, err
	}
	return x, nil
}
func (x *xlsxExportWriter) Row(row []any) error {
	x.row++
	if x.row > 1048576 {
		return &APIError{"export_row_limit", "XLSX row limit reached; no partial workbook will be offered"}
	}
	if _, err := fmt.Fprintf(x.sheet, `<row r="%d">`, x.row); err != nil {
		return err
	}
	for i, value := range row {
		if value == nil {
			continue
		} // An absent cell is SQL NULL; an inline empty string is distinct in OOXML.
		text, err := exportText(value)
		if err != nil {
			return err
		}
		if !utf8.ValidString(text) {
			return errors.New("export text is not valid UTF-8")
		}
		if len(utf16.Encode([]rune(text))) > exportMaxCellChars {
			return &APIError{"export_cell_limit", "An XLSX cell exceeds 32,767 characters; choose CSV to retain the full value"}
		}
		if _, err = fmt.Fprintf(x.sheet, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">`, xlsxColumn(i+1), x.row); err != nil {
			return err
		}
		if err = xml.EscapeText(x.sheet, []byte(xlsxString(text))); err != nil {
			return err
		}
		if _, err = io.WriteString(x.sheet, `</t></is></c>`); err != nil {
			return err
		}
	}
	_, err := io.WriteString(x.sheet, `</row>`)
	return err
}
func (x *xlsxExportWriter) Close() error {
	if _, err := io.WriteString(x.sheet, `</sheetData></worksheet>`); err != nil {
		return err
	}
	if err := x.sheet.Flush(); err != nil {
		return err
	}
	return x.zip.Close()
}
func xlsxColumn(index int) string {
	out := ""
	for index > 0 {
		index--
		out = string(rune('A'+index%26)) + out
		index /= 26
	}
	return out
}
func xlsxString(text string) string {
	var out strings.Builder
	for i, r := range text {
		// Preserve a literal escape sequence rather than letting Excel decode it.
		if r == '_' && i+7 <= len(text) && text[i+1] == 'x' && text[i+6] == '_' {
			if _, err := strconv.ParseUint(text[i+2:i+6], 16, 16); err == nil {
				out.WriteString("_x005F_")
				continue
			}
		}
		if r < 32 && r != '\t' && r != '\n' && r != '\r' || r == 0xfffe || r == 0xffff {
			fmt.Fprintf(&out, "_x%04X_", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}
