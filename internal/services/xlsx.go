package services

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// XLSX 读取（仅首个工作表）：只用标准库 archive/zip + encoding/xml，不引入新依赖。
// 业务依据：附录 A「XLSX 取首个工作表，首行即表头」。
//
// 实现说明：XLSX 是 OOXML 包。这里只做「值」的读取——单元格文本取自
// sharedStrings.xml（t="s"）或内联字符串（t="inlineStr"），数值/日期用原始字面量。
// 财务样本中金额、时间均以文本写出，读取结果与 CSV 口径一致。

// xlsxSharedStrings sharedStrings.xml 结构。
// 文本位于 <si><t>…</t></si>（富文本为 <si><r><t>…</t></r>…</si>），
// 不能用 chardata 直取 <si>（会拿不到嵌套在 <t> 里的文本）。
type xlsxSharedStrings struct {
	Items []xlsxTextItem `xml:"si"`
}

// xlsxTextItem 一段文本：<t> 直取，或由多个 <r><t> 拼接。
type xlsxTextItem struct {
	T    string    `xml:"t"`
	Runs []xlsxRun `xml:"r"`
}

type xlsxRun struct {
	T string `xml:"t"`
}

func (it xlsxTextItem) text() string {
	if len(it.Runs) > 0 {
		var b strings.Builder
		for _, r := range it.Runs {
			b.WriteString(r.T)
		}
		return b.String()
	}
	return it.T
}

// xlsxSheet 工作表 XML 结构（只关心行/列/值）。
type xlsxSheet struct {
	Rows []struct {
		Cells []struct {
			Ref  string       `xml:"r,attr"`
			Type string       `xml:"t,attr"`
			V    string       `xml:"v"`
			Is   xlsxTextItem `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

// readFirstXLSXSheet 解析 XLSX 字节内容，返回首个工作表的二维字符串。
func readFirstXLSXSheet(content []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("不是有效的 XLSX（zip）文件：%v", err)
	}

	shared, err := readSharedStrings(zr)
	if err != nil {
		return nil, err
	}

	sheetName, err := firstSheetPath(zr)
	if err != nil {
		return nil, err
	}

	raw, err := readZipFile(zr, sheetName)
	if err != nil {
		return nil, err
	}
	var sheet xlsxSheet
	if err := xml.Unmarshal(raw, &sheet); err != nil {
		return nil, fmt.Errorf("解析工作表 %s 失败：%v", sheetName, err)
	}

	var out [][]string
	for _, row := range sheet.Rows {
		var cells []string
		col := 0
		for _, c := range row.Cells {
			// 显式列引用（如 C5）时按列号补齐空缺，避免错位
			if idx := columnIndex(c.Ref); idx >= 0 {
				for col < idx {
					cells = append(cells, "")
					col++
				}
			}
			cells = append(cells, xlsxCellText(c.Type, c.V, c.Is, shared))
			col++
		}
		out = append(out, cells)
	}
	return out, nil
}

// xlsxCellText 按单元格类型取文本值（共享字符串表 / 内联字符串 / 原始字面量）。
func xlsxCellText(typ, v string, inline xlsxTextItem, shared []string) string {
	switch typ {
	case "s": // 共享字符串索引
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i >= 0 && i < len(shared) {
			return shared[i]
		}
		return ""
	case "inlineStr":
		return inline.text()
	default: // n / str / b / 空：直接用字面量
		return v
	}
}

// readSharedStrings 读取 xl/sharedStrings.xml（可选，缺失时返回空表）。
func readSharedStrings(zr *zip.Reader) ([]string, error) {
	raw, err := readZipFile(zr, "xl/sharedStrings.xml")
	if err != nil {
		// 无共享字符串表是合法情况（全部内联字符串）
		return nil, nil
	}
	var ss xlsxSharedStrings
	if err := xml.Unmarshal(raw, &ss); err != nil {
		return nil, fmt.Errorf("解析 sharedStrings.xml 失败：%v", err)
	}
	out := make([]string, 0, len(ss.Items))
	for _, si := range ss.Items {
		out = append(out, si.text())
	}
	return out, nil
}

// firstSheetPath 依 workbook.xml + rels 找到首个工作表的实际路径。
// 找不到映射时回退到 xl/worksheets/sheet1.xml（绝大多数导出文件的默认位置）。
func firstSheetPath(zr *zip.Reader) (string, error) {
	raw, err := readZipFile(zr, "xl/workbook.xml")
	if err != nil {
		return "xl/worksheets/sheet1.xml", nil
	}
	var wb struct {
		Sheets []struct {
			RID string `xml:"id,attr"` // r:id
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(raw, &wb); err != nil || len(wb.Sheets) == 0 {
		return "xl/worksheets/sheet1.xml", nil
	}
	target, err := relationshipTarget(zr, wb.Sheets[0].RID)
	if err != nil || target == "" {
		return "xl/worksheets/sheet1.xml", nil
	}
	// 关系目标相对 xl/ 目录，且可能以 /xl/ 开头
	target = strings.TrimPrefix(target, "/")
	if !strings.HasPrefix(target, "xl/") {
		target = path.Join("xl", target)
	}
	return target, nil
}

// relationshipTarget 从 xl/_rels/workbook.xml.rels 取指定 rId 的目标路径。
func relationshipTarget(zr *zip.Reader, rid string) (string, error) {
	raw, err := readZipFile(zr, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return "", err
	}
	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(raw, &rels); err != nil {
		return "", err
	}
	for _, r := range rels.Rels {
		if r.ID == rid {
			return r.Target, nil
		}
	}
	return "", nil
}

func readZipFile(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("打开 %s 失败：%v", name, err)
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("XLSX 缺少 %s", name)
}

// columnIndex 把单元格引用（如 C5）解析为 0 基列号；无法解析时返回 -1。
func columnIndex(ref string) int {
	letters := strings.TrimRight(ref, "0123456789")
	if letters == "" || letters == ref {
		return -1
	}
	n := 0
	for _, ch := range strings.ToUpper(letters) {
		if ch < 'A' || ch > 'Z' {
			return -1
		}
		n = n*26 + int(ch-'A'+1)
	}
	return n - 1
}
