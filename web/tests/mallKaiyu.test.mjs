import assert from 'node:assert/strict'
import test from 'node:test'
import { parseMallWeatherMall, mallWeatherMallPatchRequest, mallWeatherCreateRequest } from '../.test-dist/mallWeather.js'

test('开域编号在店铺读取与独立修改时保留，清空后解除绑定', () => {
  const fields = { id: 1, version: 2, mallCode: 'SH-001', nameCn: '测试店', province: '上海市', city: '上海市', district: '', address: '测试路1号', kaiyuShopId: 'abc123' }
  const mall = parseMallWeatherMall({code: 0, data: fields})
  assert.equal(mall.kaiyuShopId, 'abc123')
  assert.deepEqual(mallWeatherMallPatchRequest(mall, {...fields, kaiyuShopId: ' def456 '}), {expectedMallVersion: 2, kaiyuShopId: 'def456'})
  assert.deepEqual(mallWeatherMallPatchRequest(mall, {...fields, kaiyuShopId: ''}), {expectedMallVersion: 2, kaiyuShopId: ''})
  assert.equal(mallWeatherCreateRequest({...fields, kaiyuShopId: ' abc123 '}).kaiyuShopId, 'abc123')
})
