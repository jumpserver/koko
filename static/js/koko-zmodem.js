function utf8_to_b64(rawString) {
    return btoa(unescape(encodeURIComponent(rawString)));
}

function bytesHuman(bytes, precision) {
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

function b64_to_utf8(encodeString) {
    return decodeURIComponent(escape(atob(encodeString)));
}
