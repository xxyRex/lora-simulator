function Encode(fPort, object) {
    var bytes = [];

    // IPSO VERSION
    if (object.ipso_version) {
        bytes.push(0xff);
        bytes.push(0x01);
        bytes = bytes.concat(writeProtocolVersion(object.ipso_version));
    }

    // HARDWARE VERSION
    if (object.hardware_version) {
        bytes.push(0xff);
        bytes.push(0x09);
        bytes = bytes.concat(writeHardwareVersion(object.hardware_version));
    }

    // FIRMWARE VERSION
    if (object.firmware_version) {
        bytes.push(0xff);
        bytes.push(0x0a);
        bytes = bytes.concat(writeFirmwareVersion(object.firmware_version));
    }

    // BATTERY
    if (object.hasOwnProperty('battery')) {
        bytes.push(0x01);
        bytes.push(0x75);
        bytes.push(object.battery);
    }

    if (object.hasOwnProperty('wifiData')) {
        for (var i = 0; i < object.wifiData.length; i++) {
            var item = object.wifiData[i];
            bytes.push(0x06, 0xd9);  // Channel ID and Type
            bytes = bytes.concat(writeUInt8(item.group));
            bytes = bytes.concat(writeMAC(item.mac));
            bytes = bytes.concat(writeInt8(item.rssi));
            bytes.push(item.motion_status & 0x0f);
        }
    }

    if (object.hasOwnProperty('historyData')) {
        for (var j = 0; j < object.historyData.length; j++) {
            var record = object.historyData[j];
            bytes.push(0x20, 0xce);  // Channel ID and Type
            bytes = bytes.concat(writeUInt32LE(record.timestamp));
            bytes = bytes.concat(writeInt32LE(record.longitude * 1000000));
            bytes = bytes.concat(writeInt32LE(record.latitude * 1000000));
        }
    }

    return bytes;
}

function writeUInt8(value) {
    return [value & 0xff];
}

function writeMAC(macString) {
    return macString.split(':').map(function (hex) {
        return parseInt(hex, 16);
    });
}

function writeInt8(value) {
    if (value < 0) {
        value += 0x100;
    }
    return writeUInt8(value);
}

function writeUInt32LE(value) {
    return [
        value & 0xff,
        (value >> 8) & 0xff,
        (value >> 16) & 0xff,
        (value >> 24) & 0xff,
    ];
}

function writeInt32LE(value) {
    if (value < 0) {
        value += 0x100000000;
    }
    return writeUInt32LE(value);
}

function writeProtocolVersion(version) {
    var parts = version.substr(1).split('.');
    var major = parseInt(parts[0]);
    var minor = parseInt(parts[1]);
    return [(major << 4) | minor];
}

function writeHardwareVersion(version) {
    var parts = version.substr(1).split('.');
    var major = parseInt(parts[0]);
    var minor = parseInt(parts[1]);
    return [major, minor << 4];
}

function writeFirmwareVersion(version) {
    var parts = version.substr(1).split('.');
    var major = parseInt(parts[0]);
    var minor = parseInt(parts[1]);
    return [major, minor];
}

// Usage:
var encodedBytes = Encode(1, {
    ipso_version: "1.1",
    battery: 100,
    lorawan_class: 0,
    wifiData: [{
        group: 1,
        mac: "dd:ff:ff:ff:ff:ff",
        rssi: -42,
        motion_status: 0
    }],
    historyData: [{
        timestamp: 1582605073,
        longitude: -122.4194,
        latitude: 37.7749
    }]
});

// console.log(encodedBytes)

// const decoder = require("./at101-decoder");

// ret = decoder.Decode(1, encodedBytes);

// console.log(ret)
