package service

import (
	"fmt"
	"net/http"
	"strings"
)

func checkIfNeedAssetLoginConfirm(userID, assetID, systemUserID,
	sysUsername string) (res checkAssetConfirmResponse, err error) {
	data := map[string]string{
		"user_id":         userID,
		"asset_id":        assetID,
		"system_user_id":  systemUserID,
		"system_username": sysUsername,
	}

	_, err = authClient.Post(AssetLoginConfirmURL, data, &res)
	return
}

func checkIfNeedAppConnectionConfirm(userID, assetID, systemUserID string) (bool, error) {

	return false, nil
}

func CheckConfirmStatusByRequestInfo(req requestInfo) (res confirmStatusResponse, err error) {
	err = sendRequestByRequestInfo(req, &res)
	return
}

func CancelConfirmByRequestInfo(req requestInfo) (err error) {
	res := make(map[string]interface{})
	err = sendRequestByRequestInfo(req, &res)
	return
}

func sendRequestByRequestInfo(req requestInfo, res interface{}) (err error) {
	switch strings.ToUpper(req.Method) {
	case http.MethodGet:
		_, err = authClient.Get(req.URL, res)
	case http.MethodDelete:
		_, err = authClient.Delete(req.URL, res)
	default:
		err = fmt.Errorf("unsupport method %s", req.Method)
	}
	return
}
