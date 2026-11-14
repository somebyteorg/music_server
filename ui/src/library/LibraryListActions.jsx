import React, { cloneElement } from 'react'
import { sanitizeListRestProps, TopToolbar, CreateButton } from 'react-admin'

const LibraryListActions = ({
  className,
  filters,
  resource,
  showFilter,
  displayedFilters,
  filterValues,
  ...rest
}) => {
  return (
    <TopToolbar className={className} {...sanitizeListRestProps(rest)}>
      {filters &&
        cloneElement(filters, {
          resource,
          showFilter,
          displayedFilters,
          filterValues,
          context: 'button',
        })}
      <CreateButton />
    </TopToolbar>
  )
}

export default LibraryListActions
