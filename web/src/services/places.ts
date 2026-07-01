import { ApiError } from './auth'

/**
 * One city within a country, with the number of non-archived photos taken there
 * (`internal/placesapi` — a `CityCount`). Reverse-geocoded from GPS and cached
 * server-side.
 */
export interface PlaceCity {
  city: string
  count: number
}

/**
 * One country in the place hierarchy: its photo count and its cities. The
 * country `count` can exceed the sum of its cities' counts, because it also
 * includes photos whose city is unknown (`internal/placesapi` — `CountryPlaces`).
 * `cities` is always an array (possibly empty).
 */
export interface PlaceCountry {
  country: string
  count: number
  cities: PlaceCity[]
}

const API_BASE = '/api/v1'

/** Standard backend error envelope (`internal/placesapi`). */
interface ErrorBody {
  error?: string
}

/** Extracts the backend error message from a non-OK response, if present. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as ErrorBody
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // Body was empty or not JSON; fall back to the status text below.
  }
  return res.statusText || `request failed: ${res.status}`
}

/** JSON envelope returned by `GET /api/v1/places`. */
interface PlacesResponse {
  places: PlaceCountry[]
}

/**
 * Fetches the country → city place hierarchy from `GET /api/v1/places`. Counts
 * are aggregated over non-archived photos that carry place data; countries and
 * cities are sorted by count then name. A non-empty `country` drills into that
 * one country's cities only. The session cookie is sent automatically
 * (same-origin).
 *
 * @throws ApiError with `status` 5xx so the caller can render an error state.
 */
export async function fetchPlaces(country?: string, signal?: AbortSignal): Promise<PlaceCountry[]> {
  const query = new URLSearchParams()
  if (country !== undefined && country !== '') {
    query.set('country', country)
  }
  const suffix = query.toString() === '' ? '' : `?${query.toString()}`
  const res = await fetch(`${API_BASE}/places${suffix}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  const body = (await res.json()) as PlacesResponse
  return body.places
}
