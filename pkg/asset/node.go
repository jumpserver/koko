package asset

import "golang.org/x/crypto/ssh"

/*
   {
       "id": "060ba6be-a01d-41ef-b366-384b8a012274",
       "hostname": "docker_test",
       "ip": "127.0.0.1",
       "port": 32768,
       "system_users_granted": [
           {
               "id": "fbd39f8c-fa3e-4c2b-948e-ce1e0380b4f9",
               "name": "docker_root",
               "username": "root",
               "priority": 20,
               "protocol": "ssh",
               "comment": "screencast",
               "login_mode": "auto"
           }
       ],
       "is_active": true,
       "system_users_join": "root",
       "os": null,
       "domain": null,
       "platform": "Linux",
       "comment": "screencast",
       "protocol": "ssh",
       "org_id": "",
       "org_name": "DEFAULT"
   }
*/

type Node struct {
	IP        string `json:"ip"`
	Port      string `json:"port"`
	UserName  string `json:"username"`
	PassWord  string `json:"password"`
	PublicKey ssh.Signer
}
