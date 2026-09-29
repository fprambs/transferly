import { useController, type Control, type FieldValues, type Path } from 'react-hook-form'

import { Select, type SelectProps } from './select'

/** Props of {@link FormSelect}. */
export type FormSelectProps<T extends FieldValues> = Omit<SelectProps, 'value' | 'onValueChange'> & {
  /** The form the field belongs to. */
  control: Control<T>
  /** Name of the field. */
  name: Path<T>
  /** Value to show instead of the field's, when the form derives it from other fields. */
  value?: string
}

/** A {@link Select} bound to a field of a react-hook-form form, which reads and writes the field's value. */
export function FormSelect<T extends FieldValues>({ control, name, value, ...props }: FormSelectProps<T>) {
  const { field } = useController({ control, name })
  return <Select {...props} value={value ?? (field.value as string)} onValueChange={field.onChange} />
}
