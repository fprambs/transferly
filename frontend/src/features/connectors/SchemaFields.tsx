import { Controller, useWatch, type Control, type FieldErrors, type FieldValues, type Path, type UseFormRegister } from 'react-hook-form'

import { Checkbox } from '@/components/ui/checkbox'
import { FormSelect } from '@/components/ui/form-select'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { Connector } from '@/lib/api'

import { KeyFileField } from '../sites/KeyFileField'
import type { SiteValues } from '../sites/site-form'

import { Group, Row } from './FormRows'
import { applies, defaultOf, fixedOf, passphraseOf, settle, type ConfigValues, type ConnectorField, type FieldGroup } from './schema'
import { useConnectorTexts, type ConnectorTexts } from './use-connector-texts'

/** The values a form drawn from a schema holds: the settings of the connector and the secrets typed, by field name. */
export interface SchemaValues extends FieldValues {
  /** The settings of the connector. */
  config: ConfigValues
  /** The secrets typed. */
  secrets: ConfigValues
}

/** Props of {@link SchemaFields}. */
export interface SchemaFieldsProps<T extends SchemaValues> {
  /** The connector whose schema the fields follow. */
  connector: Connector
  /** The groups of fields to draw, in order; fields of other groups are left out. */
  groups: readonly FieldGroup[]
  /** The form the fields belong to. */
  control: Control<T>
  /** Registers a plain input with the form. */
  register: UseFormRegister<T>
  /** The validation errors of the form, whose messages are the reasons of the schema. */
  errors: FieldErrors<T>
  /** The kinds of the secrets already stored, shown as kept until another value is typed. */
  saved?: readonly string[]
  /** Choices to leave out, by field name, such as the logon types a quick connection cannot use. */
  exclude?: Readonly<Record<string, readonly string[]>>
  /** Prefix of the identifiers of the inputs, so two forms on a page do not collide. */
  idPrefix: string
}

/** Time zones the browser knows, offered as suggestions. */
const timeZones = Intl.supportedValuesOf('timeZone')

/** The names of the fields drawn inside another one: the port of a host and the passphrase of a private key. */
function drawnInside(connector: Connector): Set<string> {
  return new Set(connector.fields.flatMap((field) => [field.port, field.passphrase].filter((name): name is string => name !== undefined)))
}

/** Props of the drawing of one field. */
interface FieldProps<T extends SchemaValues> extends Omit<SchemaFieldsProps<T>, 'groups'> {
  /** The field to draw. */
  field: ConnectorField
  /** The values settled so far, defaults included. */
  settled: ConfigValues
  /** The texts of the fields. */
  texts: ConnectorTexts
}

/** The message of the validation error of a setting, if any. */
function errorOf<T extends SchemaValues>(errors: FieldErrors<T>, connector: Connector, texts: ConnectorTexts, name: string | undefined): string | undefined {
  if (name === undefined) {
    return undefined
  }
  const config = errors.config as Record<string, { message?: string } | undefined> | undefined
  const secrets = errors.secrets as Record<string, { message?: string } | undefined> | undefined
  const message = config?.[name]?.message ?? secrets?.[name]?.message
  return message === undefined ? undefined : texts.reason(connector.fields.find((field) => field.name === name), message)
}

/** The explanation of a field and of its chosen value: a warning is drawn as a note. */
function hintOf(texts: ConnectorTexts, field: ConnectorField, value: string) {
  const own = texts.field(field.name)
  const warning = own.warnings?.[value]
  if (warning !== undefined) {
    return (
      <p role="note" className="rounded-md bg-warn/10 px-3 py-2 text-foreground">
        {warning}
      </p>
    )
  }
  return own.notes?.[value] ?? own.hint
}

/** One field of the schema, drawn by its kind. */
function SchemaField<T extends SchemaValues>({ connector, field, settled, texts, control, register, errors, saved = [], exclude = {}, idPrefix }: FieldProps<T>) {
  const id = `${idPrefix}-${field.name.replaceAll('_', '-')}`
  const config = (name: string) => `config.${name}` as Path<T>
  const secret = (name: string) => `secrets.${name}` as Path<T>
  const label = texts.label(field.name)
  const value = settled[field.name] ?? ''
  const fixed = fixedOf(field, settled)
  const error = errorOf(errors, connector, texts, field.name)

  switch (field.kind) {
    case 'host': {
      const port = connector.fields.find((candidate) => candidate.name === field.port)
      return (
        <Row id={id} label={label} errors={[error, errorOf(errors, connector, texts, field.port)]} hint={texts.field(field.name).hint}>
          <div className="flex items-center gap-3">
            <Input
              id={id}
              className="min-w-0 flex-1 font-mono"
              autoComplete="off"
              spellCheck={false}
              placeholder={defaultOf(field, settled) || texts.field(field.name).empty}
              aria-invalid={error !== undefined}
              {...register(config(field.name))}
            />
            {port !== undefined && applies(port, settled) && (
              <>
                <Label htmlFor={`${id}-port`} className="font-normal text-muted-foreground">
                  {texts.label(port.name)}
                </Label>
                <Input id={`${id}-port`} className="w-20 font-mono" inputMode="numeric" placeholder={defaultOf(port, settled)} {...register(config(port.name))} />
              </>
            )}
          </div>
        </Row>
      )
    }
    case 'choice': {
      const hidden = exclude[field.name] ?? []
      const options = (field.choices ?? []).filter((choice) => !hidden.includes(choice)).map((choice) => ({ value: choice, label: texts.choice(field.name, choice) }))
      return (
        <Row id={id} label={label} errors={[error]} hint={hintOf(texts, field, value)}>
          <FormSelect
            id={id}
            control={control}
            name={config(field.name)}
            value={value}
            options={field.required ? options : [{ value: '', label: texts.field(field.name).empty ?? '' }, ...options]}
          />
        </Row>
      )
    }
    case 'boolean':
      return (
        <Row id={id} label={label} errors={[error]} hint={hintOf(texts, field, value)}>
          <Controller
            control={control}
            name={config(field.name)}
            render={({ field: input }) => <Checkbox id={id} checked={value === 'true'} onCheckedChange={(checked) => input.onChange(checked ? 'true' : 'false')} />}
          />
        </Row>
      )
    case 'secret':
      return (
        <Row id={id} label={label} errors={[error]} hint={texts.field(field.name).hint}>
          <Input id={id} type="password" autoComplete="off" placeholder={saved.includes(field.name) ? texts.keep(field.name) : undefined} {...register(secret(field.name))} />
        </Row>
      )
    case 'private_key':
      return (
        <Row id={id} label={label} errors={[error]}>
          <KeyFileField
            control={control as unknown as Control<SiteValues>}
            name={`secrets.${field.name}`}
            passphraseName={`secrets.${passphraseOf(field)}`}
            id={id}
            passphraseLabel={texts.label(passphraseOf(field))}
            saved={saved.includes(field.name)}
          />
        </Row>
      )
    default:
      return (
        <Row id={id} label={label} errors={[error]} hint={texts.field(field.name).hint}>
          {fixed !== undefined ? (
            <Input key={`${id}-fixed`} id={id} className="font-mono" value={fixed} readOnly disabled />
          ) : (
            <Input
              key={id}
              id={id}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
              inputMode={field.kind === 'number' ? 'numeric' : undefined}
              list={field.kind === 'timezone' ? `${id}-zones` : undefined}
              placeholder={defaultOf(field, settled)}
              aria-invalid={error !== undefined}
              {...register(config(field.name))}
            />
          )}
          {field.kind === 'timezone' && (
            <datalist id={`${id}-zones`}>
              {timeZones.map((zone) => (
                <option key={zone} value={zone} />
              ))}
            </datalist>
          )}
        </Row>
      )
  }
}

/**
 * The fields of the schema of a connector on some groups, drawn as the site form lays them out (ADR-0022): each group in its own set of
 * rows, titled when its translations give it a title; only the fields that apply to the values typed so far, a host with its port, a
 * private key with its passphrase, a fixed value read only, and the defaults as placeholders.
 */
export function SchemaFields<T extends SchemaValues>({ groups, ...props }: SchemaFieldsProps<T>) {
  const texts = useConnectorTexts(props.connector.type)
  const values = useWatch({ control: props.control, name: 'config' as Path<T> }) as ConfigValues
  const settled = settle(props.connector, values)
  const inside = drawnInside(props.connector)
  return (
    <>
      {groups.map((group) => {
        const fields = props.connector.fields.filter((field) => field.group === group && !inside.has(field.name) && applies(field, settled))
        return (
          fields.length > 0 && (
            <Group key={group} title={texts.group(group)}>
              {fields.map((field) => (
                <SchemaField key={field.name} {...props} field={field} settled={settled} texts={texts} />
              ))}
            </Group>
          )
        )
      })}
    </>
  )
}
