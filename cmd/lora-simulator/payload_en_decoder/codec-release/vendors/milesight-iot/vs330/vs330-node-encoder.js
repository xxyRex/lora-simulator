function Encode(fPort, obj) {
    var encoded = milesightEncode(obj);
    return encoded;
}
function milesightEncode(obj) {
    var encoded = [];
    // IPSO VERSION
    if ("ipso_version" in obj) {
        encoded.push(0xff, 0x01);
        encoded.push(encodeProtocolVersion(obj.ipso_version));
    }
    // HARDWARE VERSION
    if ("hardware_version" in obj) {
        encoded.push(0xff, 0x09);
        var hwBytes = encodeHardwareVersion(obj.hardware_version);
        encoded.push(hwBytes[0], hwBytes[1]);
    }
    // FIRMWARE VERSION
    if ("firmware_version" in obj) {
        encoded.push(0xff, 0x0a);
        var fwBytes = encodeFirmwareVersion(obj.firmware_version);
        encoded.push(fwBytes[0], fwBytes[1]);
    }
    // DEVICE STATUS
    if ("device_status" in obj) {
        encoded.push(0xff, 0x0b);
        encoded.push(obj.device_status & 0xff);
    }
    // LORAWAN CLASS TYPE
    if ("lorawan_class" in obj) {
        encoded.push(0xff, 0x0f);
        encoded.push(obj.lorawan_class & 0xff);
    }
    // SERIAL NUMBER
    if ("sn" in obj) {
        encoded.push(0xff, 0x16);
        var snBytes = encodeSerialNumber(obj.sn);
        for (var i = 0; i < snBytes.length; i++) {
            encoded.push(snBytes[i]);
        }
    }
    // BATTERY
    if ("battery" in obj) {
        encoded.push(0x01, 0x75);
        encoded.push(obj.battery & 0xff);
    }
    // DISTANCE
    if ("distance" in obj) {
        encoded.push(0x02, 0x82);
        var distBytes = encodeUInt16LE(obj.distance);
        encoded.push(distBytes[0], distBytes[1]);
    }
    // OCCUPANCY
    if ("occupancy" in obj) {
        encoded.push(0x03, 0x8e);
        encoded.push(obj.occupancy & 0xff);
    }
    // CALIBRATION RESULT
    if ("calibration_result" in obj) {
        encoded.push(0x04, 0x8e);
        encoded.push(obj.calibration_result & 0xff);
    }
    return encoded;
}
/**
 * Encode UInt16 to Little Endian bytes
 * @param {number} value - The value to encode
 * @returns {Array} - 2 bytes in LE format
 */
function encodeUInt16LE(value) {
    return [value & 0xff, (value >> 8) & 0xff];
}
/**
 * Encode protocol version string to byte
 * @param {string} version - Version string like "v1.0"
 * @returns {number} - Encoded byte
 */
function encodeProtocolVersion(version) {
    var match = version.match(/v?(\d+)\.(\d+)/);
    if (match) {
        var major = parseInt(match[1], 10) & 0x0f;
        var minor = parseInt(match[2], 10) & 0x0f;
        return (major << 4) | minor;
    }
    return 0;
}
/**
 * Encode hardware version string to bytes
 * @param {string} version - Version string like "v1.0"
 * @returns {Array} - 2 bytes
 */
function encodeHardwareVersion(version) {
    var match = version.match(/v?(\d+)\.(\d+)/);
    if (match) {
        var major = parseInt(match[1], 10) & 0xff;
        var minor = parseInt(match[2], 10) & 0x0f;
        return [major, minor << 4];
    }
    return [0, 0];
}
/**
 * Encode firmware version string to bytes
 * @param {string} version - Version string like "v1.0"
 * @returns {Array} - 2 bytes
 */
function encodeFirmwareVersion(version) {
    var match = version.match(/v?(\d+)\.(\d+)/);
    if (match) {
        var major = parseInt(match[1], 10) & 0xff;
        var minor = parseInt(match[2], 10) & 0xff;
        return [major, minor];
    }
    return [0, 0];
}
/**
 * Encode serial number hex string to bytes
 * @param {string} sn - Serial number as hex string (16 characters for 8 bytes)
 * @returns {Array} - 8 bytes
 */
function encodeSerialNumber(sn) {
    var bytes = [];
    // Ensure the string is 16 characters (8 bytes)
    var paddedSn = sn || "";
    // Manual padding to replace ES6 padEnd
    while (paddedSn.length < 16) {
        paddedSn += "0";
    }
    paddedSn = paddedSn.substring(0, 16);
    for (var i = 0; i < 16; i += 2) {
        bytes.push(parseInt(paddedSn.substring(i, i + 2), 16));
    }
    return bytes;
}

