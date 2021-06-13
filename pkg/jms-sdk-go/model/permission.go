package model

type Permission struct {
	Actions []string `json:"actions"`
}

func (p *Permission) EnableConnect() bool {
	return p.haveAction(ActionConnect)
}

func (p *Permission) EnableDrive() bool {
	return p.EnableDownload() || p.EnableUpload()
}

func (p *Permission) EnableDownload() bool {
	return p.haveAction(ActionDownload)
}

func (p *Permission) EnableUpload() bool {
	return p.haveAction(ActionUpload)
}

func (p *Permission) EnableCopy() bool {
	return p.haveAction(ActionCopy)
}

func (p *Permission) EnablePaste() bool {
	return p.haveAction(ActionPaste)
}

func (p *Permission) haveAction(action string) bool {
	for _, value := range p.Actions {
		if action == ActionALL || action == value {
			return true
		}
	}
	return false
}

const (
	ActionALL            = "all"
	ActionConnect        = "connect"
	ActionUpload         = "upload_file"
	ActionDownload       = "download_file"
	ActionUploadDownLoad = "updownload"
	ActionCopy           = "clipboard_copy"
	ActionPaste          = "clipboard_paste"
	ActionCopyPaste      = "clipboard_copy_paste"
)

type ValidateResult struct {
	Ok  bool   `json:"ok"`
	Msg string `json:"msg"`
}