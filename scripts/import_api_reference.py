#!/usr/bin/env python3
"""Import the reviewed API reference into the server's embedded catalog.

Usage: python3 scripts/import_api_reference.py SOURCE [OUTPUT]
No Markdown file is needed at runtime. Route availability and permission groups
are supplied by the application, not by this reference.
"""
import copy
import json
from pathlib import Path
import re
import sys


def clean(value):
    value = re.sub(r'\[([^\]]+)\]\(#[^)]+\)', r'\1', value)
    return value.replace('`', '').replace('**', '').replace('\\|', '|').replace('<br>', '\n').strip()


def tables(text):
    result, current = [], []
    for line in text.splitlines() + ['']:
        if line.strip().startswith('|'):
            cells = [clean(v) for v in re.split(r'(?<!\\)\|', line.strip())[1:-1]]
            if not all(re.fullmatch(r'[-: ]+', c) for c in cells):
                current.append(cells)
        elif current:
            result.append(current)
            current = []
    return result


def prose_text(text):
    # Keep each rule inline, including table column labels, without dead links.
    for table in tables(text):
        header = table[0]
        rows = ['；'.join(f'{label}：{value}' for label, value in zip(header, row)) for row in table[1:]]
        pattern = r'(?m)^\s*\|[^\n]+\n(?:\s*\|[^\n]+\n?)*'
        text = re.sub(pattern, lambda _: '\n'.join(rows) + '\n', text, count=1)
    text = re.sub(r'(?m)^#{2,5} ', '', text)
    text = re.sub(r'(?m)^---\s*$', '', text)
    text = re.sub(r'\n{3,}', '\n\n', text)
    return clean(text)


def prose(text):
    # Format prose separately so JavaScript template literals and other code
    # contents remain intact when the frontend renders the note as plain text.
    parts = re.split(r'^```[^\n]*\n(.*?)^```[ \t]*$', text, flags=re.M | re.S)
    return '\n\n'.join(
        part.strip() if index % 2 else prose_text(part)
        for index, part in enumerate(parts) if part.strip()
    )


def accepts_null(rule):
    # Only recognize explicit default/omission rules, not mentions such as
    # "不接受 null" or "数组元素不能为 null".
    return bool(re.search(
        r'(?:省略|0)\s*[/、]\s*null|null\s*/\s*(?:省略|空字符串)|'
        r'null\s*(?:时)?(?:采用|默认|使用默认)', rule
    ))


def fields(text, request=False, filters=False):
    result = []
    for table in tables(text):
        header = table[0]
        if not header or header[0] != '字段':
            continue
        for row in table[1:]:
            values = dict(zip(header, row))
            name = values['字段']
            if name == '其他字段':
                continue
            kind = values.get('类型', values.get('JSON类型', values.get('JSON 类型', 'object')))
            optionality = values.get('空值 / 可选性', '')
            required = values.get('必填', '') == '是' if request else '省略' not in optionality
            description = []
            for label, value in values.items():
                if label not in ('字段', '类型', 'JSON类型', 'JSON 类型', '必填', '位置', '可为null', '可为 null'):
                    description.append(value if label in ('含义与约束', '含义', '规则') else f'{label}：{value}')
            field = dict(name=name, type=kind, required=False if filters else required, description='\n'.join(description))
            field['nullable'] = ('null' in kind or '是' in values.get('可为null', values.get('可为 null', '')) or ('null' in optionality and '不为 null' not in optionality and '不返回 null' not in optionality))
            if request:
                field['location'] = values.get('位置', 'body')
                field['nullable'] = field['nullable'] or accepts_null(field['description'])
            result.append(field)
    return result


def nest(flat):
    result = []
    for original in flat:
        field = copy.deepcopy(original)
        parts = field['name'].split('.')
        children = result
        for index, part in enumerate(parts):
            name = part.replace('[]', '')
            existing = next((f for f in children if f['name'] == name), None)
            if index == len(parts) - 1:
                field['name'] = name
                if existing:
                    old_children = existing.get('children')
                    existing.update(field)
                    if old_children:
                        existing['children'] = old_children
                else:
                    children.append(field)
            else:
                if not existing:
                    existing = dict(name=name, type='object[]' if '[]' in part else 'object', required=True, description='对象字段，展开查看完整定义。')
                    children.append(existing)
                children = existing.setdefault('children', [])
    return result


def first_json(text):
    match = re.search(r'```json\s*\n(.*?)```', text, re.S)
    return json.loads(match[1]) if match else {}


def errors(text):
    result = []
    for table in tables(text):
        if table[0][0] != 'HTTP':
            continue
        for row in table[1:]:
            if row[0].isdigit():
                result.append(dict(status=int(row[0]), code=row[1], description='；'.join(row[2:])))
    return result


def import_reference(source):
    blocks = dict(re.findall(r'<a id="([^"]+)"></a>(.*?)(?=<a id=|\Z)', source, re.S))
    schemas = {key[7:]: nest(fields(body)) for key, body in blocks.items() if key.startswith('schema-')}
    schema_notes = {}
    for key, body in blocks.items():
        if key.startswith('schema-') and '**使用说明：**' in body:
            schema_notes[key[7:]] = prose(body.split('**使用说明：**', 1)[1].split('|', 1)[0])

    def expand(items, used, stack=()):
        for field in items:
            kind = re.sub(r'\[\]|\s*\|\s*null', '', field['type']).strip()
            if kind in schemas and kind not in stack:
                used.add(kind)
                field['children'] = copy.deepcopy(schemas[kind])
                expand(field['children'], used, (*stack, kind))
            elif field.get('children'):
                expand(field['children'], used, stack)

    common = [dict(title=re.search(r'^## (.+)', blocks[key], re.M)[1], body=prose(blocks[key])) for key in ('field-relations', 'conventions', 'field-rules', 'limits')]
    operations = []
    for key, body in blocks.items():
        route = re.search(r'\*\*接口：\*\* `(\w+) ([^`]+)`', body)
        if not route:
            continue
        title = re.search(r'^### (.+)', body, re.M)[1]
        permission = re.search(r'\*\*功能权限：\*\* `([^`]+)`', body)[1]
        sections = dict(re.findall(r'^#### ([^\n]+)\n(.*?)(?=^#### |\Z)', body, re.M | re.S))
        if key.startswith('weather-legacy-'):
            standard = key.replace('weather-legacy-', 'weather-')
            op = copy.deepcopy(next(o for o in operations if o['id'] == standard))
            overrides = fields(body.split('#### cURL')[0], request=True)
            names = {f['name'] for f in overrides}
            op['requestFields'] = overrides + [f for f in op['requestFields'] if f['name'] not in names]
            op['requestFields'][0]['example'] = 7
            op['requestExample'].pop('mallId', None)
            op['description'] = '商场 ID 由路径提供；完整请求、响应和规则均在本页。新接入建议使用 JSON Body 提供 mallId 的标准路径。'
            op['notes'].insert(0, dict(title='兼容路径规则', body='路径 id 为正整数商场内部 ID。Body 中 mallId 省略或为 0 时使用路径 id；非 0 时必须与路径 id 一致，否则返回 422。其他必填字段仍需提供。'))
        else:
            request = fields(sections.get('请求参数', ''), request=True)
            request += fields(sections.get('全部可选精确过滤字段', ''), request=True, filters=True)
            for field in request:
                # Some request rules (for example timeField's default) live in
                # an adjacent paragraph rather than the parameter table.
                rules = re.findall(rf'^`{re.escape(field["name"])}`\s*([^。\n]+)', body, re.M)
                for rule in rules:
                    if accepts_null(clean(rule)):
                        field['nullable'] = True
                        field['description'] += '\n' + clean(rule)
            response = nest(fields(sections.get('响应字段', sections.get('响应结构', ''))))
            used = set()
            expand(response, used)
            if key.startswith('tmall-'):
                ref = re.search(r'#schema-(tmall-[^)]+)', body)[1]
                items = next(f for f in next(f for f in response if f['name'] == 'data')['children'] if f['name'] == 'items')
                items['children'] = copy.deepcopy(schemas[ref])
                used.add(ref)
            if key == 'bojun-stock-snapshots':
                data = next(f for f in response if f['name'] == 'data')
                next(f for f in data['children'] if f['name'] == 'items')['children'] = copy.deepcopy(schemas['bojun-stock-snapshot'])
            description_match = re.search(r'\*\*用途：\*\* ([^\n]+)', body)
            op = dict(description=clean(description_match[1]) if description_match else title,
                      requestFields=nest(request), responseFields=response,
                      requestExample=first_json(sections.get('请求体示例', '')),
                      responseExample=first_json(sections.get('完整成功响应示例', '')),
                      errors=errors(blocks['errors']) + errors(sections.get('业务错误', '')),
                      notes=[])
            if key.startswith('tmall-'):
                op['errors'] += errors(blocks['tmall-errors'])
                op['notes'].append(dict(title='天猫数据通用规则', body=prose(blocks['group-tmall'])))
                metadata = re.findall(r'\*\*(源表|返回字段数)：\*\* ([^\n]+)', body)
                op['notes'].append(dict(title='数据来源', body='\n'.join(f'{k}：{clean(v)}' for k, v in metadata)))
            for heading, content in sections.items():
                if heading not in ('请求参数', '全部可选精确过滤字段', '请求体示例', 'cURL 示例', '响应字段', '响应结构', '完整成功响应示例', '业务错误'):
                    op['notes'].append(dict(title=heading, body=prose(content)))
            example_blocks = re.findall(r'```json\s*\n(.*?)```', sections.get('请求体示例', ''), re.S)
            if len(example_blocks) > 1:
                op['notes'].append(dict(title='其他请求示例', body='\n\n'.join(example_blocks[1:])))
            for schema in sorted(used):
                if schema in schema_notes:
                    op['notes'].append(dict(title=f'{schema} 使用说明', body=schema_notes[schema]))
            op['notes'] += copy.deepcopy(common)
        if key.startswith('bojun-'):
            for note in op['notes']:
                note['body'] = re.sub(
                    r'只校验\s+(bojun\.[a-z.]+)',
                    r'校验二级权限 \1（授权一级 bojun.read 后继承该权限）',
                    note['body'],
                )
        op.update(id=key, method=route[1], path=route[2], title=title, permission=permission,
                  authentication='请求头 token 填写唯一、完整的 dg_open_ 开放接口凭证；不加 Bearer 前缀。接口文档公开可读，业务调用仍需有效凭证及对应一级或二级权限。商场等数据范围继续生效。')
        header_fields = [
            dict(name='token', type='string', required=True, location='header', description='唯一、完整的 dg_open_ 开放接口凭证；不加 Bearer 前缀。'),
            dict(name='Content-Type', type='string', required=True, location='header', description='application/json'),
        ]
        existing = {field['name'] for field in op['requestFields']}
        op['requestFields'] = [field for field in header_fields if field['name'] not in existing] + op['requestFields']
        operations.append(op)
    if len(operations) != 33:
        raise ValueError(f'Expected 33 APIs, found {len(operations)}')
    for op in operations:
        if not op['requestFields'] or not op['responseFields'] or not op['responseExample']:
            raise ValueError(f'Incomplete API: {op["id"]}')
    return dict(title='allblu开放接口文档', version='2.0', operations=operations)


if __name__ == '__main__':
    destination = Path(sys.argv[2] if len(sys.argv) > 2 else 'internal/apidoc/catalog.json')
    catalog = import_reference(Path(sys.argv[1]).read_text())
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(json.dumps(catalog, ensure_ascii=False, indent=2) + '\n')
    print(f'Imported {len(catalog["operations"])} APIs into {destination}')
