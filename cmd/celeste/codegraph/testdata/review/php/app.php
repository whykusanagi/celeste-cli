<?php

trait Logs
{
    public function log($m)
    {
        echo $m;
    }
}

class Square implements Shape
{
    use Logs;

    public function __construct()
    {
    }

    public function area()
    {
        return 4;
    }

    public function checkout()
    {
        // TODO: charge the card
    }
}

function deadFunction()
{
}

function main()
{
    $s = new Square();
    $s->log($s->area());
}
