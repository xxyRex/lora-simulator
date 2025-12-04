#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
从codec配置文件中提取测试数据的脚本
"""

import json
import argparse
import os


def extract_test_data(codec_file, output_file, count=None):
    """
    从codec文件中提取id和value字段生成测试数据
    
    Args:
        codec_file: codec配置文件路径
        output_file: 输出的测试数据文件路径
        count: 提取的字段个数，None表示提取全部
    """
    # 读取codec文件
    with open(codec_file, 'r', encoding='utf-8') as f:
        codec_data = json.load(f)
    
    # 提取object数组
    objects = codec_data.get('object', [])
    
    # 限制提取个数
    if count is not None:
        objects = objects[:count]
    
    # 构建测试数据字典
    test_data = {}
    for obj in objects:
        obj_id = obj.get('id', '')
        obj_value = obj.get('value', '')
        
        if obj_id:
            # 尝试转换value为合适的类型
            if obj_value == '':
                # 如果value为空，保持为空字符串
                test_data[obj_id] = ''
            else:
                # 尝试转换为数字
                try:
                    # 尝试转换为整数
                    if '.' not in obj_value:
                        test_data[obj_id] = int(obj_value)
                    else:
                        # 转换为浮点数
                        test_data[obj_id] = float(obj_value)
                except (ValueError, AttributeError):
                    # 如果转换失败，保持为字符串
                    test_data[obj_id] = obj_value
    
    # 写入输出文件
    with open(output_file, 'w', encoding='utf-8') as f:
        json.dump(test_data, f, indent=4, ensure_ascii=False)
    
    print(f"成功提取 {len(test_data)} 个字段到 {output_file}")
    print(f"字段列表: {', '.join(test_data.keys())}")


def main():
    parser = argparse.ArgumentParser(
        description='从codec配置文件中提取id和value字段生成测试数据',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例用法:
  # 提取全部字段
  python extract_test_data.py -i wt201-codec.json -o test-data.json
  
  # 提取前5个字段
  python extract_test_data.py -i wt201-codec.json -o test-data.json -c 5
  
  # 提取前10个字段
  python extract_test_data.py -i wt201-codec.json -o test-data.json --count 10
        """
    )
    
    parser.add_argument(
        '-i', '--input',
        required=True,
        help='输入的codec配置文件路径'
    )
    
    parser.add_argument(
        '-o', '--output',
        required=True,
        help='输出的测试数据文件路径'
    )
    
    parser.add_argument(
        '-c', '--count',
        type=int,
        default=None,
        help='提取的字段个数（默认提取全部）'
    )
    
    args = parser.parse_args()
    
    # 检查输入文件是否存在
    if not os.path.exists(args.input):
        print(f"错误: 输入文件 {args.input} 不存在")
        return 1
    
    try:
        extract_test_data(args.input, args.output, args.count)
        return 0
    except Exception as e:
        print(f"错误: {str(e)}")
        import traceback
        traceback.print_exc()
        return 1


if __name__ == '__main__':
    exit(main())

