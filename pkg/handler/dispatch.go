package handler

import (
	"io"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

func (h *InteractiveHandler) Dispatch() {
	defer logger.Infof("Request %s: User %s stop interactive", h.sess.ID(), h.user.Name)
	var initialed bool
	checkChan := make(chan bool)
	go h.checkMaxIdleTime(checkChan)
	for {
		checkChan <- true
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Debugf("User %s close connect %s", h.user.Name, err)
			break
		}
		checkChan <- false
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			// 当 只是回车 空字符单独处理
			if initialed {
				h.selectHandler.MoveNextPage()
			} else {
				h.selectHandler.SetSelectType(TypeAsset)
				h.selectHandler.Search("")
			}
			initialed = true
			continue
		}
		initialed = true
		switch len(line) {
		case 1:
			switch strings.ToLower(line) {
			case "p":
				h.selectHandler.SetSelectType(TypeAsset)
				h.selectHandler.Search("")
				continue
			case "b":
				h.selectHandler.MovePrePage()
				continue
			case "h":
				h.selectHandler.SetSelectType(TypeHost)
				h.selectHandler.Search("")
				continue
			case "d":
				h.selectHandler.SetSelectType(TypeDatabase)
				h.selectHandler.Search("")
				continue
			case "n":
				h.selectHandler.MoveNextPage()
				continue
			case "g":
				h.wg.Wait() // 等待node加载完成
				h.displayNodeTree(h.nodes)
				continue
			case "?":
				h.displayHelp()
				initialed = false
				continue
			case "s":
				h.ChangeLang()
				h.displayHelp()
				initialed = false
				continue
			case "r":
				h.refreshAssetsAndNodesData()
				continue
			case "q":
				logger.Infof("user %s enter %s to exit", h.user.Name, line)
				return
			case "k":
				h.selectHandler.SetSelectType(TypeK8s)
				h.selectHandler.Search("")
				continue
			}
		default:
			switch {
			case line == "exit", line == "quit":
				logger.Infof("user %s enter %s to exit", h.user.Name, line)
				return
			case strings.Index(line, "/") == 0:
				if strings.Index(line[1:], "/") == 0 {
					line = strings.TrimSpace(line[2:])
					h.selectHandler.SearchAgain(line)
					continue
				}
				line = strings.TrimSpace(line[1:])
				h.selectHandler.Search(line)
				continue
			case strings.Index(line, "g") == 0:
				searchWord := strings.TrimSpace(strings.TrimPrefix(line, "g"))
				if num, err := strconv.Atoi(searchWord); err == nil {
					h.wg.Wait() // 等待node加载完成
					if num > 0 && num <= len(h.nodes) {
						selectedNode := h.nodes[num-1]
						h.selectHandler.SetNode(selectedNode)
						h.selectHandler.Search("")
						continue
					}
				}
			}
		}
		h.selectHandler.SearchOrProxy(line)
	}
}

func (h *InteractiveHandler) checkMaxIdleTime(checkChan <-chan bool) {
	maxIdleMinutes := h.terminalConf.MaxIdleTime
	checkMaxIdleTime(maxIdleMinutes, h.i18nLang, h.user, h.sess.Sess, checkChan)
}

func (h *InteractiveHandler) ChangeLang() {
	lang := i18n.NewLang(h.i18nLang)
	i18nLang := h.i18nLang
	allLangCodes := []i18n.LanguageCode{i18n.EN, i18n.ZH, i18n.ZHHant, i18n.JA, i18n.PtBr}
	langs := []string{"English", "中文", "繁體中文", "日本語", "Português"}
	idLabel := lang.T("ID")
	nameLabel := lang.T("Name")
	labels := []string{idLabel, nameLabel}
	fields := []string{"ID", "Name"}
	data := make([]map[string]string, len(langs))
	for i, j := range langs {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Name"] = j
		data[i] = row
	}
	w, _ := h.GetPtySize()
	table := common.WrapperTable{
		Fields: fields,
		Labels: labels,
		FieldsSize: map[string][3]int{
			"ID":   {0, 0, 5},
			"Name": {0, 8, 0},
		},
		Data:        data,
		TotalSize:   w,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	h.term.SetPrompt("ID> ")
	selectTip := lang.T("Tips: switch language by ID")
	backTip := lang.T("Back: B/b")
	for i := 0; i < 3; i++ {
		utils.IgnoreErrWriteString(h.term, table.Display())
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(selectTip, utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(backTip, utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
		line, err := h.term.ReadLine()
		if err != nil {
			logger.Errorf("User %s switch language err %s", h.user.Name, err)
			break
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q", "b", "quit", "exit", "back":
			logger.Infof("User %s switch language exit", h.user.Name)
			return
		case "":
			continue
		}
		if num, err2 := strconv.Atoi(line); err2 == nil {
			if num > 0 && num <= len(allLangCodes) {
				lang = allLangCodes[num-1]
				i18nLang = lang.String()
				break
			} else {
				utils.IgnoreErrWriteString(h.term, utils.WrapperString(lang.T("Invalid ID"), utils.Red))
				utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
			}
		}
	}
	if i18nLang != h.i18nLang {
		utils.IgnoreErrWriteString(h.term, utils.WrapperString(lang.T("Switch language successfully"), utils.Green))
		utils.IgnoreErrWriteString(h.term, utils.CharNewLine)
	}
	userLangGlobalStore.Store(h.user.ID, i18nLang)
	h.i18nLang = i18nLang
}

func (h *InteractiveHandler) displayNodeTree(nodes model.NodeList) {
	lang := i18n.NewLang(h.i18nLang)
	tree, newNodes := ConstructNodeTree(nodes)
	h.nodes = newNodes
	_, _ = io.WriteString(h.term, "\n\r"+lang.T("Node: [ ID.Name(Asset amount) ]"))
	_, _ = io.WriteString(h.term, tree.String())
	_, err := io.WriteString(h.term, lang.T("Tips: Enter g+NodeID to display the host under the node, such as g1")+"\n\r")
	if err != nil {
		logger.Errorf("displayAssetNodes err: %s", err)
	}
}
