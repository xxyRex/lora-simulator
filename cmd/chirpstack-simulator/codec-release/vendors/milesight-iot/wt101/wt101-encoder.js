/**
 * Payload Encoder for Milesight Network Server
 *
 * Copyright 2023 Milesight IoT
 *
 * @product WT101
 */
function Encode(decoded) {
    var bytes = [];

    if (decoded.ipso_version) {
        bytes.push(0xff, 0x01);
        bytes.push(encodeProtocolVersion(decoded.ipso_version));
    }

    if (decoded.hardware_version) {
        bytes.push(0xff, 0x09);
        bytes = bytes.concat(encodeHardwareVersion(decoded.hardware_version));
    }

    if (decoded.firmware_version) {
        bytes.push(0xff, 0x0a);
        bytes = bytes.concat(encodeFirmwareVersion(decoded.firmware_version));
    }

    if (decoded.device_status) {
        bytes.push(0xff, 0x0b, 1);
    }

    if (decoded.lorawan_class) {
        bytes.push(0xff, 0x0f, decoded.lorawan_class);
    }

    if (decoded.sn) {
        bytes.push(0xff, 0x16);
        bytes = bytes.concat(encodeSerialNumber(decoded.sn));
    }

    if (decoded.battery) {
        bytes.push(0x01, 0x75, decoded.battery);
    }

    if (decoded.temperature) {
        bytes.push(0x03, 0x67);
        bytes = bytes.concat(encodeInt16LE(decoded.temperature * 10));
    }

    if (decoded.temperature_target) {
        bytes.push(0x04, 0x67);
        bytes = bytes.concat(encodeInt16LE(decoded.temperature_target * 10));
    }

    if (decoded.valve_opening) {
        bytes.push(0x05, 0x92, decoded.valve_opening);
    }

    if (decoded.tamper_status) {
        bytes.push(0x06, 0x00, decoded.tamper_status);
    }

    if (decoded.window_detection) {
        bytes.push(0x07, 0x00, decoded.window_detection);
    }

    if (decoded.motor_calibration_result) {
        bytes.push(0x08, 0xe5, decoded.motor_calibration_result);
    }

    if (decoded.motor_storke) {
        bytes.push(0x09, 0x90);
        bytes = bytes.concat(encodeUInt16LE(decoded.motor_storke));
    }

    if (decoded.freeze_protection) {
        bytes.push(0x0a, 0x00, decoded.freeze_protection);
    }

    if (decoded.motor_position) {
        bytes.push(0x0b, 0x90);
        bytes = bytes.concat(encodeUInt16LE(decoded.motor_position));
    }

    return bytes;
}

function encodeUInt16LE(value) {
    var bytes = [];
    bytes.push(value & 0xff);
    bytes.push((value >> 8) & 0xff);
    return bytes;
}

function encodeInt16LE(value) {
    var bytes = [];
    bytes.push(value & 0xff);
    bytes.push((value >> 8) & 0xff);
    return bytes;
}

function encodeProtocolVersion(version) {
    var match = /^v(\d+)\.(\d+)$/.exec(version);
    if (match) {
        var major = parseInt(match[1], 10);
        var minor = parseInt(match[2], 10);
        return (major << 4) | minor;
    }
    return 0;
}

function encodeHardwareVersion(version) {
    var match = /^v(\d+)\.(\d+)$/.exec(version);
    if (match) {
        var major = parseInt(match[1], 10);
        var minor = parseInt(match[2], 10);
        return [major, (minor << 4)];
    }
    return [0, 0];
}

function encodeFirmwareVersion(version) {
    var match = /^v(\d+)\.(\d+)$/.exec(version);
    if (match) {
        var major = parseInt(match[1], 10);
        var minor = parseInt(match[2], 10);
        return [major, minor];
    }
    return [0, 0];
}

function encodeSerialNumber(sn) {
    var bytes = [];
    for (var i = 0; i < sn.length; i += 2) {
        bytes.push(parseInt(sn.substr(i, 2), 16));
    }
    return bytes;
}

var encodedBytes = Encode({
    ipso_version: 'v1.1',
    hardware_version: 'v2.3',
    firmware_version: 'v4.5',
    device_status: 1,
    lorawan_class: 0xA,
    sn: 'ABCDEF0123456789',
    battery: 100,
    temperature: 25.5,
    temperature_target: 20.0,
    valve_opening: 80,
    tamper_status: 0,
    window_detection: 1,
    motor_calibration_result: 0,
    motor_storke: 1000,
    freeze_protection: 1,
    motor_position: 500
});

// console.log(encodedBytes)

// const decoder = require("./wt101-decoder");

// ret = decoder.Decode(1, encodedBytes);

// console.log(ret)
