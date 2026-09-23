package services

import (
	"archive/zip"
	"bytes"
	"testing"
)

// buildTestXLSX 构造最小 XLSX：共享字符串表 + 内联字符串各覆盖一次（表头走共享串，
// 时间列走内联串），用于验证 XLSX 读取路径。
func buildTestXLSX(t *testing.T) string {
	t.Helper()
	shared := `<?xml version="1.0"?><sst><si><t>交易时间</t></si><si><t>转入金额</t></si><si><t>余额</t></si></sst>`
	sheet := `<?xml version="1.0"?><worksheet><sheetData>` +
		`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c></row>` +
		`<row r="2"><c r="A2" t="inlineStr"><is><t>2026-07-10 02:45:36</t></is></c><c r="B2"><v>11970.00</v></c><c r="C2"><v>657872.62</v></c></row>` +
		`</sheetData></worksheet>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("xl/workbook.xml", `<?xml version="1.0"?><workbook xmlns:r="r"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", `<?xml version="1.0"?><Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`)
	add("xl/sharedStrings.xml", shared)
	add("xl/worksheets/sheet1.xml", sheet)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
