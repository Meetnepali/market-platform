import type { ReactNode } from 'react'

export interface Column<T> {
  key: string
  header: ReactNode
  align?: 'left' | 'right' | 'center'
  width?: string
  render: (row: T) => ReactNode
}

interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  rowKey: (row: T) => string
  onRowClick?: (row: T) => void
  rowClassName?: (row: T) => string | undefined
  emptyMessage?: string
  /** Caps the scrollable body height so long lists never grow the page.
   *  Pass `false` to let a short table (e.g. 10 rows) size naturally. */
  maxHeight?: string | false
  className?: string
}

/**
 * The one table used everywhere in the app: sticky header, its own
 * internal vertical scroll (bounded by maxHeight) so a 300+ row
 * watchlist never forces the whole page to scroll, and horizontal
 * scroll on narrow screens without breaking layout.
 */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  onRowClick,
  rowClassName,
  emptyMessage = 'Nothing to show yet.',
  maxHeight = '60vh',
  className,
}: DataTableProps<T>) {
  if (rows.length === 0) {
    return (
      <div className="empty-state">
        <p>{emptyMessage}</p>
      </div>
    )
  }

  return (
    <div
      className={`table-wrapper${className ? ` ${className}` : ''}`}
      style={maxHeight ? { maxHeight, overflowY: 'auto' } : undefined}
    >
      <table>
        <thead>
          <tr>
            {columns.map((c) => (
              <th
                key={c.key}
                className={c.align === 'right' ? 'text-right' : c.align === 'center' ? 'text-center' : undefined}
                style={c.width ? { width: c.width } : undefined}
              >
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr
              key={rowKey(row)}
              className={[onRowClick ? 'clickable' : '', rowClassName?.(row) ?? ''].filter(Boolean).join(' ') || undefined}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
            >
              {columns.map((c) => (
                <td
                  key={c.key}
                  className={c.align === 'right' ? 'text-right' : c.align === 'center' ? 'text-center' : undefined}
                >
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
