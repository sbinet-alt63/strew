package common

type DB interface {
   Subscribers(list string) ([]string, error)
   Subscribe(user, list string) error
   Usubscribe(user, list string) error
   Lists() ([]string, error)
   Users() ([]string, error)
}