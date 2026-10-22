import { useId, type ComponentProps, type ReactNode } from 'react'
import { Field, FieldLabel, FieldDescription } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'

type FieldProps = { label: string; hint?: string }
export function TextField({ label, hint, ...props }: FieldProps & ComponentProps<typeof Input>) {
 const id = useId()
 return <Field data-invalid={props['aria-invalid'] || undefined}><FieldLabel htmlFor={id}>{label}</FieldLabel><Input id={id} name={props.name || id} autoComplete="off" {...props} />{hint && <FieldDescription>{hint}</FieldDescription>}</Field>
}
export function SelectField({ label, hint, children, ...props }: FieldProps & ComponentProps<typeof NativeSelect> & { children: ReactNode }) {
 const id = useId()
 return <Field data-invalid={props['aria-invalid'] || undefined}><FieldLabel htmlFor={id}>{label}</FieldLabel><NativeSelect id={id} name={props.name || id} {...props}>{children}</NativeSelect>{hint && <FieldDescription>{hint}</FieldDescription>}</Field>
}
