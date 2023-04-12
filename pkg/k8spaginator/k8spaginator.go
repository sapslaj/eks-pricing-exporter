package k8spaginator

import (
	"context"
)

type ListFunc[V any] func(context.Context, string) ([]V, string, error)

func New[V any](listFunc ListFunc[V]) *Paginator[V] {
	return &Paginator[V]{
		listFunc: listFunc,
	}
}

func NewListFunc[V any](listFunc ListFunc[V]) *Paginator[V] {
	return &Paginator[V]{
		listFunc: listFunc,
	}
}

type Paginator[V any] struct {
	listFunc ListFunc[V]
}

func (p *Paginator[V]) ListFunc(listFunc ListFunc[V]) *Paginator[V] {
	p.listFunc = listFunc
	return p
}

func (p *Paginator[V]) Get(ctx context.Context) ([]V, error) {
	result := make([]V, 0)
	var cont string
	for {
		items, cont, err := p.listFunc(ctx, cont)
		if err != nil {
			return result, err
		}
		result = append(result, items...)
		if cont == "" {
			break
		}
	}
	return result, nil
}
