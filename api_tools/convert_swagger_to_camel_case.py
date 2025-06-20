#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Swagger字段名转换脚本
将swagger JSON文件中的字段名从下划线命名转换为驼峰命名
例如: payload_codec_object_id -> payloadCodecObjectId
"""

import json
import re
import sys
import os
from typing import Any, Dict, List, Union


def snake_to_camel(snake_str: str) -> str:
    """
    将下划线命名转换为驼峰命名
    例如: payload_codec_object_id -> payloadCodecObjectId
    """
    if not snake_str or '_' not in snake_str:
        return snake_str
    
    components = snake_str.split('_')
    # 第一个组件保持小写，其余组件首字母大写
    return components[0] + ''.join(word.capitalize() for word in components[1:])


def convert_object_keys(obj: Any) -> Any:
    """
    递归转换对象中的所有键名
    """
    if isinstance(obj, dict):
        new_obj = {}
        for key, value in obj.items():
            # 转换键名
            new_key = snake_to_camel(key)
            # 递归处理值
            new_obj[new_key] = convert_object_keys(value)
        return new_obj
    elif isinstance(obj, list):
        return [convert_object_keys(item) for item in obj]
    else:
        return obj


def convert_property_names_in_definitions(definitions: Dict[str, Any]) -> Dict[str, Any]:
    """
    转换definitions中的属性名
    """
    new_definitions = {}
    
    for def_name, def_content in definitions.items():
        new_def_content = {}
        
        for key, value in def_content.items():
            if key == "properties" and isinstance(value, dict):
                # 转换properties中的字段名
                new_properties = {}
                for prop_name, prop_content in value.items():
                    new_prop_name = snake_to_camel(prop_name)
                    new_properties[new_prop_name] = convert_object_keys(prop_content)
                new_def_content[key] = new_properties
            else:
                new_def_content[key] = convert_object_keys(value)
        
        new_definitions[def_name] = new_def_content
    
    return new_definitions


def convert_parameter_names(parameters: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """
    转换API参数中的字段名
    """
    new_parameters = []
    
    for param in parameters:
        new_param = {}
        for key, value in param.items():
            if key == "name" and isinstance(value, str):
                # 转换参数名
                new_param[key] = snake_to_camel(value)
            else:
                new_param[key] = convert_object_keys(value)
        new_parameters.append(new_param)
    
    return new_parameters


def convert_swagger_file(input_file: str, output_file: str = None) -> None:
    """
    转换swagger文件
    """
    if not os.path.exists(input_file):
        print(f"错误: 输入文件 {input_file} 不存在")
        return
    
    try:
        # 读取原始文件
        with open(input_file, 'r', encoding='utf-8') as f:
            swagger_data = json.load(f)
        
        print(f"正在处理文件: {input_file}")
        
        # 转换definitions中的字段名
        if "definitions" in swagger_data:
            print("转换definitions中的字段名...")
            swagger_data["definitions"] = convert_property_names_in_definitions(
                swagger_data["definitions"]
            )
        
        # 转换paths中的参数名
        if "paths" in swagger_data:
            print("转换API路径中的参数名...")
            for path, methods in swagger_data["paths"].items():
                for method, details in methods.items():
                    if "parameters" in details:
                        details["parameters"] = convert_parameter_names(details["parameters"])
        
        # 确定输出文件名
        if output_file is None:
            # 在原文件名基础上添加_camel后缀
            base_name = os.path.splitext(input_file)[0]
            extension = os.path.splitext(input_file)[1]
            output_file = f"{base_name}_camel{extension}"
        
        # 写入转换后的文件
        with open(output_file, 'w', encoding='utf-8') as f:
            json.dump(swagger_data, f, indent=2, ensure_ascii=False)
        
        print(f"转换完成! 输出文件: {output_file}")
        
        # 显示一些转换示例
        print("\n转换示例:")
        examples = [
            "payload_codec_object_id",
            "server_id", 
            "register_addr",
            "register_type",
            "register_num",
            "data_type",
            "update_time",
            "device_name",
            "dev_eui"
        ]
        
        for example in examples:
            converted = snake_to_camel(example)
            if converted != example:
                print(f"  {example} -> {converted}")
    
    except json.JSONDecodeError as e:
        print(f"错误: JSON解析失败 - {e}")
    except Exception as e:
        print(f"错误: {e}")


def main():
    """
    主函数
    """
    if len(sys.argv) < 2:
        print("用法: python convert_swagger_to_camel_case.py <input_file> [output_file]")
        print("示例: python convert_swagger_to_camel_case.py modbus_slave.swagger.json")
        print("示例: python convert_swagger_to_camel_case.py modbus_slave.swagger.json modbus_slave_camel.swagger.json")
        return
    
    input_file = sys.argv[1]
    output_file = sys.argv[2] if len(sys.argv) > 2 else None
    
    convert_swagger_file(input_file, output_file)


if __name__ == "__main__":
    main() 