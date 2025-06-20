#!/usr/bin/env python3
import json
import re

# 读取合并后的 swagger 文件
with open('api.swagger.json', 'r') as f:
    swagger = json.load(f)

# 用于记录已使用的 operationId
used_ids = set()

# 遍历所有路径和方法
for path, methods in swagger['paths'].items():
    for method, operation in methods.items():
        # 从路径生成有意义的操作 ID
        parts = [p for p in path.split('/') if p and not p.startswith('{')]
        
        # 添加请求路径中的参数名
        param_names = re.findall(r'\{([^}]+)\}', path)
        if param_names:
            parts.append('by_' + '_'.join(param_names))
            
        # 基本 ID
        base_id = method.lower() + '_' + '_'.join(parts)
        
        # 确保唯一性
        operation_id = base_id
        counter = 1
        while operation_id in used_ids:
            operation_id = f"{base_id}_{counter}"
            counter += 1
            
        # 记录并设置
        used_ids.add(operation_id)
        operation['operationId'] = operation_id

# 保存修改后的文件
with open('api.fixed.json', 'w') as f:
    json.dump(swagger, f, indent=2)