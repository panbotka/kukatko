import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'
import { createSearchParams, useNavigate } from 'react-router-dom'

/**
 * Compact search box in the navbar. Submitting navigates to the dedicated search
 * route with the query in the URL (`/search?q=…`), so the result is shareable
 * and Back returns to where the user was. The search page itself owns debouncing
 * and mode selection; this is just a prominent entry point present on every page.
 */
export function NavbarSearch() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [text, setText] = useState('')

  return (
    <Form
      role="search"
      aria-label={t('search.formLabel')}
      className="d-flex me-md-2 my-2 my-md-0"
      onSubmit={(e) => {
        e.preventDefault()
        const q = text.trim()
        if (q === '') {
          return
        }
        void navigate({ pathname: '/search', search: `?${createSearchParams({ q }).toString()}` })
      }}
    >
      <Form.Control
        type="search"
        size="sm"
        className="flex-grow-1"
        value={text}
        placeholder={t('search.placeholder')}
        aria-label={t('search.queryLabel')}
        onChange={(e) => {
          setText(e.target.value)
        }}
      />
      <Button type="submit" size="sm" variant="outline-light" className="ms-2">
        {t('search.submit')}
      </Button>
    </Form>
  )
}
