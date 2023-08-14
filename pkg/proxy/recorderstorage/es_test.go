package recorderstorage

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEsIndexResponse(t *testing.T) {
	respBodys := [][2]string{
		{"index", `{"took":24,"errors":false,"items":[{"index":{"_index":"jumpserver-test-1","_type":"_doc","_id":"mo9R9IkBIDTIizd_N0BL","_version":1,"result":"created","_shards":{"total":1,"successful":1,"failed":0},"_seq_no":3,"_primary_term":1,"status":201}},{"index":{"_index":"jumpserver-test-1","_type":"_doc","_id":"m49R9IkBIDTIizd_N0BL","_version":1,"result":"created","_shards":{"total":1,"successful":1,"failed":0},"_seq_no":4,"_primary_term":1,"status":201}}]}`},
		{"create", `{"took":36,"errors":false,"items":[{"create":{"_index":"jumpserver-test-1","_type":"_doc","_id":"mI9Q9IkBIDTIizd_5UBF","_version":1,"result":"created","_shards":{"total":1,"successful":1,"failed":0},"_seq_no":1,"_primary_term":1,"status":201}},{"create":{"_index":"jumpserver-test-1","_type":"_doc","_id":"mY9Q9IkBIDTIizd_5UBF","_version":1,"result":"created","_shards":{"total":1,"successful":1,"failed":0},"_seq_no":2,"_primary_term":1,"status":201}}]}`},
	}

	for idx := range respBodys {
		action, data := respBodys[idx][0], respBodys[idx][1]
		var (
			blk  *bulkResponse
			body bytes.Buffer = *bytes.NewBufferString(data)
		)

		if err := json.NewDecoder(&body).Decode(&blk); err != nil {
			t.Fatalf("ES failure to parse response body: %s", err)
		} else {
			for _, d := range blk.Items {
				if _, ok := d[action]; !ok {
					t.Fatalf("can not get action response from es bulk response body %d", idx)
				}
			}
		}
	}
}
