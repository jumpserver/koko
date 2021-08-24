
const scheme = document.location.protocol === "https:" ? "wss" : "ws";
const port = document.location.port ? ':' + document.location.port : '';
const BASE_WS_URL = scheme + '://' + document.location.hostname + port;
const BASE_URL = document.location.protocol+ '//'+ document.location.hostname + port;
export { BASE_WS_URL, BASE_URL }

export function decodeToStr(octets) {
    if (typeof TextEncoder == "function") {
        return new TextDecoder("utf-8").decode(new Uint8Array(octets))
    }
    return decodeURIComponent(escape(String.fromCharCode.apply(null, octets)));
}

export function fireEvent(e) {
    window.dispatchEvent(e)
}

export function bytesHuman(bytes, precision) {
    if (!/^([-+])?|(\.\d+)(\d+(\.\d+)?|(\d+\.)|Infinity)$/.test(bytes)) {
        return '-'
    }
    if (bytes === 0) return '0';
    if (typeof precision === 'undefined') precision = 1;
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB', 'BB'];
    const num = Math.floor(Math.log(bytes) / Math.log(1024));
    const value = (bytes / Math.pow(1024, Math.floor(num))).toFixed(precision);
    return `${value} ${units[num]}`
}

export function CopyTextToClipboard(text){
    let transfer = document.createElement('textarea');
    document.body.appendChild(transfer);
    transfer.value =text;
    transfer.focus();
    transfer.select();
    document.execCommand('copy');
    document.body.removeChild(transfer);
}