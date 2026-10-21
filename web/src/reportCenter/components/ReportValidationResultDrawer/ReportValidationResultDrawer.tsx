import { Drawer } from '../../../ui'
import type { ReportPublication } from '../../types'
import { ReportValidationResultContent } from './ReportValidationResultContent'

export function ReportValidationResultDrawer({ publication, onClose }: { publication: ReportPublication | null; onClose: () => void }) {
  return <Drawer open={Boolean(publication)} title="发布测试结果" description={publication ? `版本 #${publication.version} 已形成不可变契约；以下结果来自本次发布核验。` : undefined} size="medium" onClose={onClose} footer={<button type="button" onClick={onClose}>完成</button>}>
    {publication ? <ReportValidationResultContent publication={publication} /> : null}
  </Drawer>
}
