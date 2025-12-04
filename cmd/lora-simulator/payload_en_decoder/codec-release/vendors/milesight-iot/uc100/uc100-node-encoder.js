/**
 * Payload Encoder
 *
 * Copyright 2025 Milesight IoT
 *
 * @product UC100
 */

function Encode(fPort, obj) {
    var encoded = milesightEncode(obj);
    return encoded;
}

function milesightEncode(obj) {
    var encoded = [];

    // IPSO VERSION (0xff, 0x01)
    if ("ipso_version" in obj) {
        encoded.push(0xff, 0x01);
        encoded.push(encodeProtocolVersion(obj.ipso_version));
    }

    // HARDWARE VERSION (0xff, 0x09)
    if ("hardware_version" in obj) {
        encoded.push(0xff, 0x09);
        var hwBytes = encodeHardwareVersion(obj.hardware_version);
        encoded.push(hwBytes[0], hwBytes[1]);
    }

    // FIRMWARE VERSION (0xff, 0x0a)
    if ("firmware_version" in obj) {
        encoded.push(0xff, 0x0a);
        var fwBytes = encodeFirmwareVersion(obj.firmware_version);
        encoded.push(fwBytes[0], fwBytes[1]);
    }

    // TSL VERSION (0xff, 0xff)
    if ("tsl_version" in obj) {
        encoded.push(0xff, 0xff);
        var tslBytes = encodeTslVersion(obj.tsl_version);
        encoded.push(tslBytes[0], tslBytes[1]);
    }

    // SERIAL NUMBER (0xff, 0x16)
    if ("sn" in obj) {
        encoded.push(0xff, 0x16);
        var snBytes = encodeSerialNumber(obj.sn);
        for (var i = 0; i < snBytes.length; i++) {
            encoded.push(snBytes[i]);
        }
    }

    // LORAWAN CLASS TYPE (0xff, 0x0f)
    if ("lorawan_class" in obj) {
        encoded.push(0xff, 0x0f);
        encoded.push(obj.lorawan_class & 0xff);
    }

    // RESET EVENT (0xff, 0xfe)
    if ("reset_event" in obj) {
        encoded.push(0xff, 0xfe);
        encoded.push(obj.reset_event & 0xff);
    }

    // DEVICE STATUS (0xff, 0x0b)
    if ("device_status" in obj) {
        encoded.push(0xff, 0x0b);
        encoded.push(obj.device_status & 0xff);
    }

    // MODBUS CHANNELS (0xff, 0x19)
    // Format: modbus_chn_1, modbus_chn_2, etc.
    for (var key in obj) {
        if (obj.hasOwnProperty(key)) {
            var match = key.match(/^modbus_chn_(\d+)$/);
            if (match && !key.endsWith("_alarm") && !key.endsWith("_mutation")) {
                var chnId = parseInt(match[1], 10);
                var value = obj[key];
                var dataType = obj[key + "_data_type"] || 2; // Default to INT16
                var sign = obj[key + "_sign"] || 0; // Default to unsigned

                encoded.push(0xff, 0x19);
                encoded.push((chnId - 1) & 0xff); // channel_id (0-based)

                var dataBytes = encodeModbusValue(value, dataType, sign);
                encoded.push(dataBytes.length & 0xff); // data_length
                var dataDef = ((sign & 0x01) << 7) | (dataType & 0x7f);
                encoded.push(dataDef);

                for (var j = 0; j < dataBytes.length; j++) {
                    encoded.push(dataBytes[j]);
                }
            }
        }
    }

    // MODBUS READ ERROR (0xff, 0x15)
    for (var key in obj) {
        if (obj.hasOwnProperty(key)) {
            var match = key.match(/^modbus_chn_(\d+)_alarm$/);
            if (match) {
                var chnId = parseInt(match[1], 10);
                var alarmValue = obj[key];
                // Only encode as read error if it's a read error type (value 1)
                if (alarmValue === 1) {
                    encoded.push(0xff, 0x15);
                    encoded.push((chnId - 1) & 0xff);
                }
            }
        }
    }

    // MODBUS ALARM (0xff, 0xee) - for threshold alarms (v1.7+)
    for (var key in obj) {
        if (obj.hasOwnProperty(key)) {
            var match = key.match(/^modbus_chn_(\d+)_alarm$/);
            if (match) {
                var chnId = parseInt(match[1], 10);
                var alarmType = obj[key];
                // Handle threshold alarm (1) or threshold release alarm (2)
                if (alarmType === 1 || alarmType === 2) {
                    var valueKey = "modbus_chn_" + chnId;
                    if (valueKey in obj) {
                        var value = obj[valueKey];
                        var dataType = obj[valueKey + "_data_type"] || 2;
                        var sign = obj[valueKey + "_sign"] || 0;

                        var chnDef = ((alarmType & 0x03) << 6) | ((chnId - 1) & 0x3f);
                        encoded.push(0xff, 0xee);
                        encoded.push(chnDef);

                        var dataBytes = encodeModbusValue(value, dataType, sign);
                        encoded.push(dataBytes.length & 0xff);
                        var dataDef = ((sign & 0x01) << 7) | (dataType & 0x7f);
                        encoded.push(dataDef);

                        for (var j = 0; j < dataBytes.length; j++) {
                            encoded.push(dataBytes[j]);
                        }
                    }
                }
            }
        }
    }

    // MODBUS MUTATION (0xf9, 0x5f) - v1.9+
    for (var key in obj) {
        if (obj.hasOwnProperty(key)) {
            var match = key.match(/^modbus_chn_(\d+)_mutation$/);
            if (match) {
                var chnId = parseInt(match[1], 10);
                var mutationValue = obj[key];
                var alarmKey = "modbus_chn_" + chnId + "_alarm";

                if (alarmKey in obj && obj[alarmKey] === 3) {
                    var chnDef = (3 << 6) | ((chnId - 1) & 0x3f);
                    encoded.push(0xf9, 0x5f);
                    encoded.push(chnDef);
                    encoded.push(0x00, 0x00); // placeholder bytes
                    var floatBytes = encodeFloatLE(mutationValue);
                    for (var j = 0; j < floatBytes.length; j++) {
                        encoded.push(floatBytes[j]);
                    }
                }
            }
        }
    }

    // MODBUS HISTORY (0x20, 0xce) - v1.7+
    if ("history" in obj && Array.isArray(obj.history)) {
        for (var h = 0; h < obj.history.length; h++) {
            var historyItem = obj.history[h];

            // Check if it's a custom message history
            if ("custom_message" in historyItem) {
                // CUSTOM MESSAGE HISTORY (0x20, 0xcd)
                encoded.push(0x20, 0xcd);
                var tsBytes = encodeUInt32LE(historyItem.timestamp || 0);
                for (var t = 0; t < tsBytes.length; t++) {
                    encoded.push(tsBytes[t]);
                }
                var msgBytes = encodeAscii(historyItem.custom_message);
                encoded.push(msgBytes.length & 0xff);
                for (var m = 0; m < msgBytes.length; m++) {
                    encoded.push(msgBytes[m]);
                }
            } else {
                // MODBUS HISTORY
                for (var histKey in historyItem) {
                    if (historyItem.hasOwnProperty(histKey)) {
                        var histMatch = histKey.match(/^modbus_chn_(\d+)$/);
                        if (histMatch) {
                            var histChnId = parseInt(histMatch[1], 10);
                            var histValue = historyItem[histKey];
                            var histDataType = historyItem[histKey + "_data_type"] || 2;
                            var histSign = historyItem[histKey + "_sign"] || 0;
                            var histReadStatus = historyItem[histKey + "_alarm"] ? 0 : 1;

                            encoded.push(0x20, 0xce);
                            var tsBytes = encodeUInt32LE(historyItem.timestamp || 0);
                            for (var t = 0; t < tsBytes.length; t++) {
                                encoded.push(tsBytes[t]);
                            }
                            encoded.push((histChnId - 1) & 0xff);

                            var histDataDef = ((histSign & 0x01) << 7) | ((histDataType & 0x1f) << 2) | ((histReadStatus & 0x01) << 1);
                            encoded.push(histDataDef);

                            if (histReadStatus === 0) {
                                // Read failed - 4 bytes padding
                                encoded.push(0x00, 0x00, 0x00, 0x00);
                            } else {
                                var histDataBytes = encodeModbusHistoryValue(histValue, histDataType, histSign);
                                for (var j = 0; j < histDataBytes.length; j++) {
                                    encoded.push(histDataBytes[j]);
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // CUSTOM MESSAGE (raw message at end)
    if ("custom_message" in obj && !("history" in obj)) {
        var msgBytes = encodeAscii(obj.custom_message);
        for (var m = 0; m < msgBytes.length; m++) {
            encoded.push(msgBytes[m]);
        }
    }

    // DOWNLINK RESPONSE - Report Interval (0xff/0xfe, 0x03)
    if ("report_interval" in obj) {
        encoded.push(0xff, 0x03);
        var intervalBytes = encodeUInt16LE(obj.report_interval);
        encoded.push(intervalBytes[0], intervalBytes[1]);
    }

    // DOWNLINK RESPONSE - Confirm Mode (0xff/0xfe, 0x04)
    if ("confirm_mode_enable" in obj) {
        encoded.push(0xff, 0x04);
        encoded.push(obj.confirm_mode_enable & 0xff);
    }

    // DOWNLINK RESPONSE - Reboot (0xff/0xfe, 0x10)
    if ("reboot" in obj) {
        encoded.push(0xff, 0x10);
        encoded.push(obj.reboot & 0xff);
    }

    // DOWNLINK RESPONSE - Clear History (0xff/0xfe, 0x27)
    if ("clear_history" in obj) {
        encoded.push(0xff, 0x27);
        encoded.push(obj.clear_history & 0xff);
    }

    // DOWNLINK RESPONSE - History Enable (0xff/0xfe, 0x68)
    if ("history_enable" in obj) {
        encoded.push(0xff, 0x68);
        encoded.push(obj.history_enable & 0xff);
    }

    // DOWNLINK RESPONSE - Retransmit Enable (0xff/0xfe, 0x69)
    if ("retransmit_enable" in obj) {
        encoded.push(0xff, 0x69);
        encoded.push(obj.retransmit_enable & 0xff);
    }

    // DOWNLINK RESPONSE - Retransmit/Resend Interval (0xff/0xfe, 0x6a)
    if ("retransmit_interval" in obj) {
        encoded.push(0xff, 0x6a);
        encoded.push(0x00);
        var retransmitBytes = encodeUInt16LE(obj.retransmit_interval);
        encoded.push(retransmitBytes[0], retransmitBytes[1]);
    }

    if ("resend_interval" in obj) {
        encoded.push(0xff, 0x6a);
        encoded.push(0x01);
        var resendBytes = encodeUInt16LE(obj.resend_interval);
        encoded.push(resendBytes[0], resendBytes[1]);
    }

    // DOWNLINK RESPONSE - Modbus Channel Operations (0xff/0xfe, 0xef)
    // Remove Modbus Channels
    if ("remove_modbus_channels" in obj && Array.isArray(obj.remove_modbus_channels)) {
        for (var r = 0; r < obj.remove_modbus_channels.length; r++) {
            var removeItem = obj.remove_modbus_channels[r];
            encoded.push(0xff, 0xef);
            encoded.push(0x00); // Remove operation
            encoded.push(removeItem.channel_id & 0xff);
            encoded.push(0x00, 0x00); // Padding
        }
    }

    // Add Modbus Channels
    if ("modbus_channels" in obj && Array.isArray(obj.modbus_channels)) {
        for (var a = 0; a < obj.modbus_channels.length; a++) {
            var addItem = obj.modbus_channels[a];
            encoded.push(0xff, 0xef);
            encoded.push(0x01); // Add operation
            var channelBytes = encodeModbusChannels(addItem);
            for (var c = 0; c < channelBytes.length; c++) {
                encoded.push(channelBytes[c]);
            }
        }
    }

    // Modify Modbus Channels Name
    if ("modbus_channels_name" in obj && Array.isArray(obj.modbus_channels_name)) {
        for (var n = 0; n < obj.modbus_channels_name.length; n++) {
            var nameItem = obj.modbus_channels_name[n];
            encoded.push(0xff, 0xef);
            encoded.push(0x02); // Modify name operation
            encoded.push(nameItem.channel_id & 0xff);
            var nameBytes = encodeAscii(nameItem.name);
            encoded.push(nameBytes.length & 0xff);
            for (var nb = 0; nb < nameBytes.length; nb++) {
                encoded.push(nameBytes[nb]);
            }
        }
    }

    return encoded;
}

/**
 * Encode Modbus value based on data type
 */
function encodeModbusValue(value, dataType, sign) {
    switch (dataType) {
        case 0: // Coil
        case 1: // Discrete
            return [value & 0xff];
        case 2: // INT16_AB
        case 3: // INT16_BA
            if (sign) {
                return encodeInt16LE(value);
            }
            return encodeUInt16LE(value);
        case 4: // INT32_ABCD
        case 6: // INT32_CDAB
            if (sign) {
                return encodeInt32LE(value);
            }
            return encodeUInt32LE(value);
        case 8:  // INT32_AB
        case 9:  // INT32_CD
        case 10: // INT32_AB (variant)
        case 11: // INT32_CD (variant)
            // These read 16-bit but occupy 4 bytes in payload
            var bytes16 = sign ? encodeInt16LE(value) : encodeUInt16LE(value);
            return [bytes16[0], bytes16[1], 0x00, 0x00];
        case 5: // FLOAT_BADC
        case 7: // FLOAT_DCBA
            return encodeFloatLE(value);
        default:
            if (sign) {
                return encodeInt16LE(value);
            }
            return encodeUInt16LE(value);
    }
}

/**
 * Encode Modbus history value based on data type
 */
function encodeModbusHistoryValue(value, dataType, sign) {
    switch (dataType) {
        case 0:  // MB_REG_COIL
        case 1:  // MB_REG_DISCRETE
            return [value & 0xff, 0x00, 0x00, 0x00];
        case 2:  // MB_REG_INPUT_AB
        case 3:  // MB_REG_INPUT_BA
        case 14: // MB_REG_HOLD_INT16_AB
        case 15: // MB_REG_HOLD_INT16_BA
            var bytes32 = sign ? encodeInt32LE(value) : encodeUInt32LE(value);
            return bytes32;
        case 4:  // MB_REG_INPUT_INT32_ABCD
        case 5:  // MB_REG_INPUT_INT32_BADC
        case 6:  // MB_REG_INPUT_INT32_CDAB
        case 7:  // MB_REG_INPUT_INT32_DCBA
        case 16: // MB_REG_HOLD_INT32_ABCD
        case 17: // MB_REG_HOLD_INT32_BADC
        case 18: // MB_REG_HOLD_INT32_CDAB
        case 19: // MB_REG_HOLD_INT32_DCBA
            return sign ? encodeInt32LE(value) : encodeUInt32LE(value);
        case 8:  // MB_REG_INPUT_INT32_AB
        case 9:  // MB_REG_INPUT_INT32_CD
        case 20: // MB_REG_HOLD_INT32_AB
        case 21: // MB_REG_HOLD_INT32_CD
            var bytes16 = sign ? encodeInt16LE(value) : encodeUInt16LE(value);
            return [bytes16[0], bytes16[1], 0x00, 0x00];
        case 10: // MB_REG_INPUT_FLOAT_ABCD
        case 11: // MB_REG_INPUT_FLOAT_BADC
        case 12: // MB_REG_INPUT_FLOAT_CDAB
        case 13: // MB_REG_INPUT_FLOAT_DCBA
        case 22: // MB_REG_HOLD_FLOAT_ABCD
        case 23: // MB_REG_HOLD_FLOAT_BADC
        case 24: // MB_REG_HOLD_FLOAT_CDAB
        case 25: // MB_REG_HOLD_FLOAT_DCBA
            return encodeFloatLE(value);
        default:
            return sign ? encodeInt32LE(value) : encodeUInt32LE(value);
    }
}

/**
 * Encode Modbus channel configuration
 */
function encodeModbusChannels(channel) {
    var bytes = [];
    bytes.push(channel.channel_id & 0xff);
    bytes.push(channel.slave_id & 0xff);
    var addrBytes = encodeUInt16LE(channel.register_address);
    bytes.push(addrBytes[0], addrBytes[1]);
    bytes.push(encodeRegisterType(channel.register_type) & 0xff);
    var signBit = channel.sign === "signed" || channel.sign === 1 ? 1 : 0;
    var quantity = channel.quantity & 0x0f;
    bytes.push((signBit << 4) | quantity);
    return bytes;
}

/**
 * Encode register type string to number
 */
function encodeRegisterType(type) {
    var registerTypeMap = {
        "MB_REG_COIL": 0,
        "MB_REG_DIS": 1,
        "MB_REG_INPUT_AB": 2,
        "MB_REG_INPUT_BA": 3,
        "MB_REG_INPUT_INT32_ABCD": 4,
        "MB_REG_INPUT_INT32_BADC": 5,
        "MB_REG_INPUT_INT32_CDAB": 6,
        "MB_REG_INPUT_INT32_DCBA": 7,
        "MB_REG_INPUT_INT32_AB": 8,
        "MB_REG_INPUT_INT32_CD": 9,
        "MB_REG_INPUT_FLOAT_ABCD": 10,
        "MB_REG_INPUT_FLOAT_BADC": 11,
        "MB_REG_INPUT_FLOAT_CDAB": 12,
        "MB_REG_INPUT_FLOAT_DCBA": 13,
        "MB_REG_HOLD_INT16_AB": 14,
        "MB_REG_HOLD_INT16_BA": 15,
        "MB_REG_HOLD_INT32_ABCD": 16,
        "MB_REG_HOLD_INT32_BADC": 17,
        "MB_REG_HOLD_INT32_CDAB": 18,
        "MB_REG_HOLD_INT32_DCBA": 19,
        "MB_REG_HOLD_INT32_AB": 20,
        "MB_REG_HOLD_INT32_CD": 21,
        "MB_REG_HOLD_FLOAT_ABCD": 22,
        "MB_REG_HOLD_FLOAT_BADC": 23,
        "MB_REG_HOLD_FLOAT_CDAB": 24,
        "MB_REG_HOLD_FLOAT_DCBA": 25,
        "MB_REG_INPUT_DOUBLE_ABCDEFGH": 26,
        "MB_REG_INPUT_DOUBLE_GHEFCDAB": 27,
        "MB_REG_INPUT_DOUBLE_BADCFEHG": 28,
        "MB_REG_INPUT_DOUBLE_HGFEDCBA": 29,
        "MB_REG_INPUT_INT64_ABCDEFGH": 30,
        "MB_REG_INPUT_INT64_GHEFCDAB": 31,
        "MB_REG_INPUT_INT64_BADCFEHG": 32,
        "MB_REG_INPUT_INT64_HGFEDCBA": 33,
        "MB_REG_HOLD_DOUBLE_ABCDEFGH": 34,
        "MB_REG_HOLD_DOUBLE_GHEFCDAB": 35,
        "MB_REG_HOLD_DOUBLE_BADCFEHG": 36,
        "MB_REG_HOLD_DOUBLE_HGFEDCBA": 37,
        "MB_REG_HOLD_INT64_ABCDEFGH": 38,
        "MB_REG_HOLD_INT64_GHEFCDAB": 39,
        "MB_REG_HOLD_INT64_BADCFEHG": 40,
        "MB_REG_HOLD_INT64_HGFEDCBA": 41
    };

    if (typeof type === "number") {
        return type;
    }
    return registerTypeMap[type] || 0;
}

// ============ Encoding Helper Functions ============

/**
 * Encode UInt8
 */
function encodeUInt8(value) {
    return value & 0xff;
}

/**
 * Encode UInt16 to Little Endian bytes
 */
function encodeUInt16LE(value) {
    return [value & 0xff, (value >> 8) & 0xff];
}

/**
 * Encode Int16 to Little Endian bytes
 */
function encodeInt16LE(value) {
    if (value < 0) {
        value = 0x10000 + value;
    }
    return [value & 0xff, (value >> 8) & 0xff];
}

/**
 * Encode UInt32 to Little Endian bytes
 */
function encodeUInt32LE(value) {
    return [
        value & 0xff,
        (value >> 8) & 0xff,
        (value >> 16) & 0xff,
        (value >> 24) & 0xff
    ];
}

/**
 * Encode Int32 to Little Endian bytes
 */
function encodeInt32LE(value) {
    if (value < 0) {
        value = 0x100000000 + value;
    }
    return [
        value & 0xff,
        (value >> 8) & 0xff,
        (value >> 16) & 0xff,
        (value >> 24) & 0xff
    ];
}

/**
 * Encode Float to Little Endian bytes (IEEE 754)
 */
function encodeFloatLE(value) {
    var buffer = new ArrayBuffer(4);
    var floatView = new Float32Array(buffer);
    var intView = new Uint8Array(buffer);
    floatView[0] = value;
    return [intView[0], intView[1], intView[2], intView[3]];
}

/**
 * Encode protocol version string to byte
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
 * Encode TSL version string to bytes
 */
function encodeTslVersion(version) {
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
 */
function encodeSerialNumber(sn) {
    var bytes = [];
    var paddedSn = sn || "";
    while (paddedSn.length < 16) {
        paddedSn += "0";
    }
    paddedSn = paddedSn.substring(0, 16);
    for (var i = 0; i < 16; i += 2) {
        bytes.push(parseInt(paddedSn.substring(i, i + 2), 16));
    }
    return bytes;
}

/**
 * Encode ASCII string to bytes
 */
function encodeAscii(str) {
    var bytes = [];
    for (var i = 0; i < str.length; i++) {
        bytes.push(str.charCodeAt(i) & 0xff);
    }
    return bytes;
}

// Export for Node.js testing
if (typeof module !== "undefined" && module.exports) {
    module.exports = {
        Encode: Encode,
        milesightEncode: milesightEncode
    };
}