let scheme = document.location.protocol == "https:" ? "wss" : "ws";
let port = document.location.port ? ":" + document.location.port : "";
let wsURL = scheme + "://" + document.location.hostname + port + "/koko/ws/";

let interval;
let roomsTermMap = {};

function handleError(reason) {
    console.log(reason);
}

function main(roomid) {
    let sshEvents = {};
    sshEvents._OnNamespaceConnected = function (nsConn, msg) {
        if (nsConn.conn.wasReconnected()) {
            console.log("re-connected after " + nsConn.conn.reconnectTries.toString() + " trie(s)");
        }

        console.log("connected to namespace: " + msg.Namespace);
        interval = setInterval(() => nsConn.emit('ping', ''), 10000);
    };

    sshEvents._OnNamespaceDisconnect = function (nsConn, msg) {
        console.log("disconnected from namespace: " + msg.Namespace);
        if (interval) {
            clearInterval(interval);
        }
    };

    sshEvents.shareRoomData = function (nsConn, msg) {
        var roomData = msg.unmarshal();
        var roomMsg = roomsTermMap[roomData.room];
        if (roomMsg == null && !roomMsg) {
            console.log("not found ", roomData.room);
            return
        }
        var term = roomMsg["term"];
        if (term != null && term) {
            term.write(roomData.data)
        }
    };

    sshEvents.logout = function (nsConn, msg) {
        var logoutMsg = msg.unmarshal();
        console.log(logoutMsg);
        var roomMsg = roomsTermMap[logoutMsg.room];
        if (roomMsg == null && !roomMsg) {
            console.log("not found ", logoutMsg.room);
            return
        }
        var term = roomMsg["term"];
        if (term != null && term) {
            term.write("\n\rroom " + logoutMsg.room + " already logout.");
            term.write("\n\rclose connection\n\r");
        }
        nsConn.conn.close();
    };

    sshEvents.room = function (nsConn, msg) {
        var roomMsg = msg.unmarshal();
        console.log(roomMsg);
        var storeMsg = {};
        roomsTermMap[roomMsg.room] = storeMsg;
        var term = createTerminalById("terminal");
        storeMsg["room"] = roomMsg.room;
        storeMsg["term"] = term;
        term.on_resize = function (cols, rows) {
            console.log(cols, rows);
            if (cols !== this.cols || rows !== this.rows) {
                console.log('Resizing terminal to geometry: ' + cols + ' ' + rows);
                nsConn.emit("winsize", JSON.stringify({'resize': [cols, rows]}));
            }
        };
        term.onData(function (data) {
            const msg = {'room': roomMsg.room, 'data': data};
            console.log(msg);
            nsConn.emit("data", JSON.stringify(msg));
        });
    };

    neffos.dial(wsURL, {"ssh": sshEvents},
        {reconnect: 5000,}
    ).then(function (conn) {
            const data = {"shareRoomID": roomid, "secret": uuidv4()};
            conn.connect("ssh").then(function (nsConn) {
                    nsConn.emit("shareRoom", JSON.stringify(data));
                }
            ).catch(handleError);
        }
    ).catch(handleError);
}


function createTerminalById(elementid) {
    let fontSize = 14;
    const ua = navigator.userAgent.toLowerCase();
    let lineHeight = 1;
    if (ua.indexOf('windows') !== -1) {
        lineHeight = 1.2;
    }
    let term = new Terminal({
        fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
        lineHeight: lineHeight,
        fontSize: fontSize,
        rightClickSelectsWord: true,
        theme: {
            background: '#1f1b1b'
        }
    });
    term.open(document.getElementById(elementid));
    return term
}
