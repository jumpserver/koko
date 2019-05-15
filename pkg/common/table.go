package common

import (
	"fmt"
	"strings"

	"github.com/olekukonko/tablewriter"
)

const (
	TruncSuffix = iota
	TruncPrefix
	TruncMiddle
)

type WrapperTable struct {
	Labels      []string
	Fields      []string
	FieldsSize  map[string][3]int // 列宽，列最小宽，列最大宽
	Data        []map[string]string
	TotalSize   int
	TruncPolicy int

	totalSize   int
	paddingSize int
	bolderSize  int
	fieldsSize  map[string]int // 计算后的最终宽度
	cleanedData []map[string]string
	Caption     string
}

func (t *WrapperTable) Initial() {
	// 如果设置的宽度小于title的size， 重写
	if t.Labels == nil {
		t.Labels = t.Fields
	}
	if len(t.Fields) != len(t.Labels) {
		panic("Labels should be equal with Fields")
	}
	t.paddingSize = 1
	t.bolderSize = 1
	for i, k := range t.Fields {
		titleSize := len(t.Labels[i])
		sizeDefine := t.FieldsSize[k]
		if titleSize > sizeDefine[1] {
			sizeDefine[1] = titleSize
			t.FieldsSize[k] = sizeDefine
		}
	}
}

func (t *WrapperTable) CalculateColumnsSize() {
	t.fieldsSize = make(map[string]int)

	dataColMaxSize := make(map[string]int)
	for _, row := range t.Data {
		for _, colName := range t.Fields {
			if colValue, ok := row[colName]; ok {
				preSize, ok := dataColMaxSize[colName]
				colSize := len(colValue)
				if !ok || colSize > preSize {
					dataColMaxSize[colName] = colSize
				}
			}
		}
	}

	// 如果数据宽度大于设置最大值，则设置为准
	// 如果数据最大值小彧最小值，已最小值为列宽
	// 否则数据最大宽度为列宽
	for k, v := range dataColMaxSize {
		size, min, max := t.FieldsSize[k][0], t.FieldsSize[k][1], t.FieldsSize[k][2]
		if size != 0 {
			t.fieldsSize[k] = size
		} else if max != 0 && v > max {
			t.fieldsSize[k] = max
		} else if min != 0 && v < min {
			t.fieldsSize[k] = min
		} else {
			t.fieldsSize[k] = v
		}
	}

	// 计算后列总长度
	calSize := 0
	for _, v := range t.fieldsSize {
		calSize += v
	}
	if t.TotalSize == 0 {
		t.totalSize = calSize
		return
	}

	// 总宽度计算时应当减去 border和padding
	t.totalSize = t.TotalSize - len(t.Fields)*2*t.paddingSize - (len(t.Fields)+1)*t.bolderSize

	// 计算可以扩容和缩容的列
	delta := t.totalSize - calSize
	if delta == 0 {
		return
	}
	var step = 1
	if delta < 0 {
		step = -1
	}
	delta = Abs(delta)
	for delta > 0 {
		canChangeCols := make([]string, 0)
		for k, v := range t.FieldsSize {
			size, min, max := v[0], v[1], v[2]
			switch step {
			// 扩容
			case 1:
				if size != 0 || (max != 0 && t.fieldsSize[k] >= max) {
					continue
				}
			// 缩容
			case -1:
				if size != 0 || t.fieldsSize[k] <= min {
					continue
				}
			}
			canChangeCols = append(canChangeCols, k)
		}
		if len(canChangeCols) == 0 {
			break
		}
		for _, k := range canChangeCols {
			t.fieldsSize[k] += step
			delta--
			if delta == 0 {
				break
			}
			fmt.Println(t.fieldsSize)
		}
		fmt.Println(canChangeCols)
	}
}

func (t *WrapperTable) convertDataToSlice() [][]string {
	data := make([][]string, len(t.Data))
	for i, j := range t.Data {
		row := make([]string, len(t.Fields))
		for m, n := range t.Fields {
			columSize := t.fieldsSize[n]
			if len(j[n]) <= columSize {
				row[m] = j[n]
			} else {
				switch t.TruncPolicy {
				case TruncSuffix:
					row[m] = j[n][:columSize-3] + "..."
				case TruncPrefix:
					row[m] = "..." + j[n][len(j[n])-columSize-3:]
				case TruncMiddle:
					midValue := (columSize - 3) / 2
					row[m] = j[n][:midValue] + "..." + j[n][len(j[n])-midValue:]
				}
			}

		}
		data[i] = row
	}
	return data
}

func (t *WrapperTable) Display() string {
	t.CalculateColumnsSize()
	fmt.Println(t.fieldsSize)

	tableString := &strings.Builder{}
	table := tablewriter.NewWriter(tableString)
	table.SetBorder(false)
	table.SetHeader(t.Labels)
	colors := make([]tablewriter.Colors, len(t.Fields))
	for i := 0; i < len(t.Fields); i++ {
		colors[i] = tablewriter.Colors{tablewriter.Bold, tablewriter.FgGreenColor}
	}
	table.SetHeaderColor(colors...)
	data := t.convertDataToSlice()
	table.AppendBulk(data)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	for i, j := range t.Fields {
		n := t.fieldsSize[j]
		table.SetColMinWidth(i, n)
	}
	table.SetColWidth(t.totalSize)
	if t.Caption != "" {
		table.SetCaption(true, t.Caption)
	}
	table.Render()
	return tableString.String()
}
