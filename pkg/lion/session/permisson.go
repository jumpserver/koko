package session

import (
	"github.com/jumpserver/koko/pkg/config"

	"github.com/jumpserver-dev/sdk-go/model"
)

type ActionPermission struct {
	EnableConnect bool `json:"enable_connect"`

	EnableCopy  bool `json:"enable_copy"`
	EnablePaste bool `json:"enable_paste"`

	EnableUpload   bool `json:"enable_upload"`
	EnableDownload bool `json:"enable_download"`
	EnableShare    bool `json:"enable_share"`

	ClipboardPolicy *model.ClipboardPolicy `json:"clipboard_policy,omitempty"`
}

func NewActionPermission(perm *model.Permission, connectType string,
	clipboardPolicy *model.ClipboardPolicy) *ActionPermission {
	action := ActionPermission{
		EnableConnect:  perm.EnableConnect(),
		EnableCopy:     perm.EnableCopy(),
		EnablePaste:    perm.EnablePaste(),
		EnableUpload:   perm.EnableUpload(),
		EnableDownload: perm.EnableDownload(),
		EnableShare:    perm.EnableShare(),
	}
	globConfig := config.GetConf()
	switch connectType {
	case TypeRemoteApp:
		if globConfig.EnableRemoteAppUpDownLoad {
			action.EnableDownload = true
			action.EnableUpload = true
		}
		if globConfig.EnableRemoteAPPCopyPaste {
			action.EnablePaste = true
			action.EnableCopy = true
		}
	case TypeRDP, TypeVNC:
	}
	if globConfig.DisableAllUpDownload {
		action.EnableDownload = false
		action.EnableUpload = false
	}
	if globConfig.DisableAllCopyPaste {
		action.EnablePaste = false
		action.EnableCopy = false
	}
	action.applyClipboardPolicy(clipboardPolicy)
	return &action
}

func (a *ActionPermission) applyClipboardPolicy(policy *model.ClipboardPolicy) {
	if policy == nil {
		return
	}
	a.ClipboardPolicy = policy
	a.EnableCopy = a.EnableCopy && clipboardPolicyItemEnabled(policy.Copy)
	a.EnablePaste = a.EnablePaste && clipboardPolicyItemEnabled(policy.Paste)
}

func clipboardPolicyItemEnabled(item *model.ClipboardPolicyItem) bool {
	if item == nil {
		return true
	}
	return item.Enabled
}
