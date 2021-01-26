"use strict";

let MaxTimeout = 30 * 1000

function message(id, type, data) {
    return JSON.stringify({
        id,
        type,
        data,
    });
}

function handleError(reason) {
    console.log(reason);
}

function initTerminal(elementId) {
    let urlParams = new URLSearchParams(window.location.search.slice(1));
    let scheme = document.location.protocol == "https:" ? "wss" : "ws";
    let port = document.location.port ? ":" + document.location.port : "";
    let baseWsUrl = scheme + "://" + document.location.hostname + port;
    let pingInterval;
    let resizeTimer;
    let term;
    let lastSendTime;
    let lastReceiveTime;
    let initialed;
    let ws;
    let terminalId = "";
    let termSelection = "";
    let wsURL = baseWsUrl + '/koko/ws/terminal/?' + urlParams.toString();
    switch (urlParams.get("type")) {
        case 'token':
            wsURL = baseWsUrl + "/koko/ws/token/?" + urlParams.toString();
            break
        default:
    }
    ws = new WebSocket(wsURL, ["JMS-KOKO"]);
    term = createTerminalById(elementId)

    function resizeTerminal() {
        // 延迟调整窗口大小
        if (resizeTimer != null) {
            clearTimeout(resizeTimer);
        }
        resizeTimer = setTimeout(function () {
            const termRef = document.getElementById('terminal')
            termRef.style.height = (window.innerHeight - 16) + 'px';
            term.fit();
            term.focus();
            let cols = term.cols;
            let rows = term.rows;
            if (initialed == null || ws == null) {
                return
            }
            ws.send(message(terminalId, 'TERMINAL_RESIZE',
                JSON.stringify({cols, rows})));
        }, 500)

    }

    function dispatch(term, data) {
        if (data === undefined) {
            return
        }
        let msg = JSON.parse(data)
        switch (msg.type) {
            case 'CONNECT':
                terminalId = msg.id;
                term.fit();
                let cols = term.cols;
                let rows = term.rows;
                ws.send(message(terminalId, 'TERMINAL_INIT',
                    JSON.stringify({cols, rows})));
                initialed = true;
                resizeTerminal();
                break
            case "CLOSE":
                term.writeln("Connection closed");
                fireEvent(new Event("CLOSE", {}))
                break
            case "PING":
                break
            case 'TERMINAL_DATA':
                term.write(msg.data);
                break
            default:
                console.log(data)
        }
    }

    window.SendTerminalData = function (data) {
        if (ws.readyState === WebSocket.CLOSING ||
            ws.readyState === WebSocket.CLOSED) {
            return
        }
        ws.send(message(terminalId, 'TERMINAL_DATA', data));
    }

    window.addEventListener('resize', resizeTerminal);

    let quickPaste = getQuickPaste();
    let terminalContext = document.getElementById(elementId);
    terminalContext.addEventListener('contextmenu', function ($event) {
        if ($event.ctrlKey || quickPaste !== '1') {
            return;
        }
        if (navigator.clipboard && navigator.clipboard.readText) {
            navigator.clipboard.readText().then((text) => {
                ws.send(message(terminalId, 'TERMINAL_DATA', text))
            })
            $event.preventDefault();
        } else if (termSelection !== "") {
            ws.send(message(terminalId, 'TERMINAL_DATA', termSelection))
            $event.preventDefault();
        }
    })

    term.on('data', data => {
        if (initialed === null || ws === null) {
            return
        }
        lastSendTime = new Date();
        ws.send(message(terminalId, 'TERMINAL_DATA', data));
    });

    term.on('selection', function () {
        document.execCommand('copy');
        // this ==> term object
        termSelection = this.getSelection().trim();
    });

    ws.onopen = () => {
        if (pingInterval !== null) {
            clearInterval(pingInterval);
        }
        lastReceiveTime = new Date();
        pingInterval = setInterval(function () {
            if (ws.readyState === WebSocket.CLOSING ||
                ws.readyState === WebSocket.CLOSED) {
                clearInterval(pingInterval)
                return
            }
            let currentDate = new Date();
            if ((lastReceiveTime - currentDate) > MaxTimeout) {
                console.log("more than 30s do not receive data")
            }
            let pingTimeout = (currentDate - lastSendTime) - MaxTimeout
            if (pingTimeout < 0) {
                return;
            }
            ws.send(message(terminalId, 'PING', ""));
        }, 25 * 1000);
    }
    ws.onerror = (e) => {
        term.writeln("Connection error");
        fireEvent(new Event("CLOSE", {}))
        handleError(e)
    }
    ws.onclose = (e) => {
        term.writeln("Connection closed");
        fireEvent(new Event("CLOSE", {}))
        handleError(e)
    }
    ws.onmessage = (e) => {
        lastReceiveTime = new Date();
        dispatch(term, e.data);
    }
}

function createTerminalById(elementId) {
    let fontSize = getFontSize();
    const termRef = document.getElementById('terminal')
    termRef.style.height = (window.innerHeight - 16) + 'px';
    fit.apply(Terminal);
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
    term.open(document.getElementById(elementId));
    term.focus();
    term.attachCustomKeyEventHandler(function (e) {
        if (e.ctrlKey && e.key === 'c' && term.hasSelection()) {
            return false;
        }
        return !(e.ctrlKey && e.key === 'v');
    });
    return term
}

function fireEvent(e) {
    window.dispatchEvent(e)
}

function getFontSize() {
    let fontSize = 14
    // localStorage.getItem default null
    let localSettings = localStorage.getItem('LunaSetting')
    if (localSettings !== null) {
        let settings = JSON.parse(localSettings)
        fontSize = settings['fontSize']
    }
    if (!fontSize || fontSize < 5 || fontSize > 50) {
        fontSize = 13;
    }
    return fontSize
}

function getQuickPaste() {
    let quickPaste = "0"
    let localSettings = localStorage.getItem('LunaSetting')
    if (localSettings !== null) {
        let settings = JSON.parse(localSettings)
        quickPaste = settings['quickPaste']
    }
    return quickPaste
}