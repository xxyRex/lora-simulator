/**
 * Payload Encoder for Milesight Network Server
 *
 * Copyright 2023 Milesight IoT
 *
 * @product UC300
 */
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

    if ('gpio_input_1' in object) {
        bytes.push(0x03);
        bytes.push(0x00);
        if (object.gpio_input_1) {
            bytes.push(0x01);
        } else {
            bytes.push(0x00);
        }
    }

    if ('gpio_output_1' in object) {
        bytes.push(0x07);
        bytes.push(0x01);
        if (object.gpio_output_1) {
            bytes.push(0x01);
        } else {
            bytes.push(0x00);
        }
    }

    return bytes;
}

function writeUInt8(value) {
    return value & 0xff;
}

function writeInt8(value) {
    return value < 0 ? value + 0x100 : value;
}

function writeUInt16LE(value) {
    return [(value & 0xff), (value >> 8) & 0xff];
}

function writeInt16LE(value) {
    return writeUInt16LE(value < 0 ? value + 0x10000 : value);
}

function writeUInt32LE(value) {
    return [
        (value & 0xff),
        (value >> 8) & 0xff,
        (value >> 16) & 0xff,
        (value >> 24) & 0xff
    ];
}

function writeInt32LE(value) {
    return writeUInt32LE(value < 0 ? value + 0x100000000 : value);
}

function writeFloatLE(value) {
    var buffer = new ArrayBuffer(4);
    var view = new DataView(buffer);
    view.setFloat32(0, value, true);
    return [view.getUint8(0), view.getUint8(1), view.getUint8(2), view.getUint8(3)];
}

function writeFloat16LE(value) {
    // Float16 is not natively supported by JavaScript, so we need to handle the conversion manually.
    var sign = value < 0 ? 1 : 0;
    value = Math.abs(value);
    var exponent = Math.floor(Math.log(value) / Math.log(2));
    var significand = ((value / Math.pow(2, exponent)) - 1) * 1024;
    exponent += 15;

    if (exponent <= 0) {
        exponent = 0;
        significand = value / Math.pow(2, -14);
    } else if (exponent >= 31) {
        exponent = 31;
        significand = 0;
    }

    var bits = (sign << 15) | (exponent << 10) | (significand & 0x3ff);
    return [(bits & 0xff), (bits >> 8) & 0xff];
}

function writeAscii(string) {
    var bytes = [];
    for (var i = 0; i < string.length; ++i) {
        bytes.push(string.charCodeAt(i));
    }
    return bytes;
}

function writeProtocolVersion(version) {
    var parts = version.substr(1).split('.');
    var major = parseInt(parts[0], 10);
    var minor = parseInt(parts[1], 10);
    return ((major << 4) | minor) & 0xff;
}

function writeHardwareVersion(version) {
    var parts = version.substr(1).split('.');
    var major = parseInt(parts[0], 10);
    var minor = parseInt(parts[1], 10);
    return [major & 0xff, (minor << 4) & 0xf0];
}

function writeFirmwareVersion(version) {
    var parts = version.substr(1).split('.');
    var major = parseInt(parts[0], 10);
    var minor = parseInt(parts[1], 10);
    return [major & 0xff, minor & 0xff];
}

function writeSerialNumber(serialNumber) {
    var bytes = [];
    for (var i = 0; i < serialNumber.length; i += 2) {
        bytes.push(parseInt(serialNumber.substr(i, 2), 16));
    }
    return bytes;
}

// 变量名必须固定为encodedBytes
var encodedBytes = Encode(1, {
    ipso_version: "v1.2",
    hardware_version: "v1.0",
    firmware_version: "v2.5",
    gpio_input_1: 1,
    gpio_output_1: 1
});

// console.log(encodedBytes)

// const decoder = require("./uc300-decoder");

// ret = decoder.Decode(1, encodedBytes);

// console.log(ret)
