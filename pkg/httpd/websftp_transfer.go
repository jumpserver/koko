package httpd

import "encoding/json"

func (h *webSftp) handleTransferRead(request *webSftpRequest, response *Message) {
	data, metadata, err := h.volume.readTransferChunk(request.TransferID, request.Path, request.OffSet, request.Length)
	if err != nil {
		h.sendError(response, err)
		return
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		h.sendError(response, err)
		return
	}
	response.Data = string(payload)
	response.Raw = data
	if request.Binary {
		response.Type = SFTPTransferBinary
	} else {
		response.Type = SFTPBinary
	}
	h.ws.SendMessage(response)
}

func (h *webSftp) handleTransferMutation(request *webSftpRequest, msg *Message, response *Message) {
	var (
		result sftpTransferResult
		err    error
	)
	switch msg.Cmd {
	case "transfer_prepare":
		result, err = h.volume.prepareTransfer(request.TransferID, request.Path, request.Size, request.ConflictPolicy)
	case "transfer_write":
		result, err = h.volume.writeTransferChunk(request.TransferID, request.Path, request.Size, request.OffSet, request.SHA256, msg.Raw)
	case "transfer_status":
		result, err = h.volume.transferStatus(request.TransferID, request.Path, request.Size)
	case "transfer_commit":
		result, err = h.volume.commitTransfer(request.TransferID, request.Path, request.Size, request.ChunkSize, request.SHA256, request.ConflictPolicy)
	case "transfer_cancel":
		result, err = h.volume.cancelTransfer(request.TransferID, request.Path, request.Discard)
	}
	if err != nil {
		h.sendError(response, err)
		return
	}
	payload, err := json.Marshal(result)
	if err != nil {
		response.Err = err.Error()
	} else {
		response.Data = string(payload)
	}
	h.ws.SendMessage(response)
}
