package model

type Actions []Action

func (a Actions) EnableConnect() bool {
	return a.haveAction(ActionConnect)
}

func (a Actions) EnableDrive() bool {
	return a.EnableDownload() || a.EnableUpload()
}

func (a Actions) EnableDownload() bool {
	return a.haveAction(ActionDownload)
}

func (a Actions) EnableUpload() bool {
	return a.haveAction(ActionUpload)
}

func (a Actions) EnableCopy() bool {
	return a.haveAction(ActionCopy)
}

func (a Actions) EnablePaste() bool {
	return a.haveAction(ActionPaste)
}

func (a Actions) EnableDelete() bool {
	return a.haveAction(ActionDelete)
}

func (a Actions) EnableShare() bool {
	return a.haveAction(ActionShare)
}

func (a Actions) haveAction(action string) bool {
	for _, actionItem := range a {
		if action == ActionALL || action == actionItem.Value {
			return true
		}
	}
	return false
}

func (a Actions) Permission() Permission {
	var permission Permission
	permission.Actions = make([]string, 0, len(a))
	for i := range a {
		permission.Actions = append(permission.Actions, a[i].Value)
	}
	return permission
}

/*
 'actions': [{'label': '连接', 'value': 'connect'},
             {'label': '上传文件', 'value': 'upload'},
             {'label': '下载文件', 'value': 'download'},
             {'label': 'Copy', 'value': 'copy'},
             {'label': 'Paste', 'value': 'paste'}],
*/
